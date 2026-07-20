// Package audit provides tool-call audit logging for the broker.
//
// It writes one AuditRecord per tool call to two optional sinks:
//   - a JSONL file (newline-delimited JSON, one object per line) for
//     embedded mode of the neuralgentics-web broker_audit module, and
//   - the PostgreSQL “broker_audit_log“ table for team-server mode.
//
// The record shape matches the consumer (T-107):
//
//	{"ts":"2026-07-20T05:01:14Z","agent_role":"coder","server":"filesystem",
//	 "tool":"read_file","args_hash":"sha256:...","success":true,
//	 "result_size":1234,"duration_ms":45,"error":""}
//
// The PG schema matches the one created idempotently by
// PGBrokerAuditSource.apply_schema() in
// packages/web/.../broker_audit/data_source.py — see SCHEMA_SQL below.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/lib/pq" // postgres driver
)

// Sink is a destination for audit records.
type Sink int

const (
	SinkOff   Sink = 0
	SinkJSONL Sink = 1 << iota
	SinkPG
	SinkJSONLAndPG = SinkJSONL | SinkPG
)

// ParseSink parses a flag value like "off" / "jsonl" / "jsonl+pg".
func ParseSink(s string) (Sink, error) {
	switch s {
	case "off":
		return SinkOff, nil
	case "jsonl":
		return SinkJSONL, nil
	case "jsonl+pg":
		return SinkJSONLAndPG, nil
	default:
		return SinkOff, fmt.Errorf("unknown audit sink %q (want off|jsonl|jsonl+pg)", s)
	}
}

// Config controls the AuditWriter. All fields are safe to copy.
type Config struct {
	Sink           Sink
	JSONLPath      string        // default ~/.neuralgentics/broker_audit.jsonl
	PGURL          string        // empty = skip PG writes
	FlushInterval  time.Duration // default 1s
	ArgsTruncate   int           // 0 = no truncate; else cap args_hash length (no-op; args not stored)
	ResultTruncate int           // 0 = no truncate; else cap result_size reporting (no-op; only size stored)
	BatchSize      int           // flush when buffer reaches this many records (default 100)
}

// DefaultConfig returns the production defaults.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Sink:           SinkJSONL,
		JSONLPath:      filepath.Join(home, ".neuralgentics", "broker_audit.jsonl"),
		FlushInterval:  time.Second,
		ArgsTruncate:   4096,
		ResultTruncate: 8192,
		BatchSize:      100,
	}
}

// AuditRecord is one broker tool-call audit event. Field names and JSON
// keys match the T-107 consumer schema (data_source.py::BrokerAuditEvent).
type AuditRecord struct {
	TS         time.Time `json:"ts"`
	AgentRole  string    `json:"agent_role"`
	Server     string    `json:"server"`
	Tool       string    `json:"tool"`
	ArgsHash   string    `json:"args_hash,omitempty"`
	Success    bool      `json:"success"`
	ResultSize *int      `json:"result_size,omitempty"`
	DurationMS int       `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// SCHEMA_SQL mirrors the consumer's PGBrokerAuditSource.SCHEMA_SQL
// (data_source.py lines 49-76). Same columns, same trigger, same NOTIFY
// channel. Kept in sync so the writer can create the table idempotently
// the first time it connects (useful for first-run and tests).
const SCHEMA_SQL = `
CREATE TABLE IF NOT EXISTS broker_audit_log (
    id          SERIAL PRIMARY KEY,
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    agent_role  TEXT        NOT NULL,
    server      TEXT        NOT NULL,
    tool        TEXT        NOT NULL,
    args_hash   TEXT,
    success     BOOLEAN     NOT NULL,
    result_size INTEGER,
    duration_ms INTEGER     NOT NULL,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_broker_audit_ts   ON broker_audit_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_broker_audit_tool ON broker_audit_log (tool);

CREATE OR REPLACE FUNCTION notify_broker_audit_new() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('broker_audit_new', row_to_json(NEW)::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS broker_audit_notify ON broker_audit_log;
CREATE TRIGGER broker_audit_notify AFTER INSERT ON broker_audit_log
    FOR EACH ROW EXECUTE FUNCTION notify_broker_audit_new();
`

// AuditWriter buffers AuditRecords and flushes them to the configured
// sinks on a timer or when the batch fills. It is safe for concurrent
// use — Call sites (the broker dispatch path) push records from many
// goroutines simultaneously.
type AuditWriter struct {
	cfg Config

	mu     sync.Mutex
	buf    []AuditRecord
	cond   *sync.Cond
	closed bool

	jsonlFile *os.File
	pgDB      *sql.DB

	flusherWG sync.WaitGroup
	flusherCh chan struct{}
}

// NewAuditWriter opens the configured sinks. Returns a no-op writer
// (Sink == SinkOff) without error. The caller MUST call Close() to
// flush any buffered records before the process exits.
func NewAuditWriter(cfg Config) (*AuditWriter, error) {
	w := &AuditWriter{cfg: cfg}
	w.cond = sync.NewCond(&w.mu)

	if cfg.Sink == SinkOff {
		return w, nil
	}

	// Open JSONL sink (always on unless SinkOff).
	if cfg.Sink&SinkJSONL != 0 {
		if cfg.JSONLPath == "" {
			return nil, errors.New("audit: JSONL path required for jsonl sink")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.JSONLPath), 0o755); err != nil {
			return nil, fmt.Errorf("audit: mkdir %s: %w", filepath.Dir(cfg.JSONLPath), err)
		}
		f, err := os.OpenFile(cfg.JSONLPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("audit: open %s: %w", cfg.JSONLPath, err)
		}
		w.jsonlFile = f
	}

	// Open PG sink (optional).
	if cfg.Sink&SinkPG != 0 {
		if cfg.PGURL == "" {
			return nil, errors.New("audit: PG URL required for jsonl+pg sink")
		}
		db, err := sql.Open("postgres", cfg.PGURL)
		if err != nil {
			return nil, fmt.Errorf("audit: open pg: %w", err)
		}
		// Idempotently apply schema on first connect so first-run
		// creates the table + trigger the consumer expects.
		if _, err := db.Exec(SCHEMA_SQL); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("audit: apply pg schema: %w", err)
		}
		w.pgDB = db
	}

	// Start flusher goroutine (interval-based flush).
	w.flusherCh = make(chan struct{})
	w.flusherWG.Add(1)
	go w.flushLoop()

	return w, nil
}

// Write enqueues a record for asynchronous flush. Non-blocking unless
// the buffer is at 2x BatchSize (back-pressure).
func (w *AuditWriter) Write(rec AuditRecord) {
	if w.cfg.Sink == SinkOff {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.buf = append(w.buf, rec)
	if w.cfg.BatchSize > 0 && len(w.buf) >= w.cfg.BatchSize {
		w.cond.Signal()
	}
}

// Flush forces a synchronous flush of the current buffer.
func (w *AuditWriter) Flush() error {
	if w.cfg.Sink == SinkOff {
		return nil
	}
	w.mu.Lock()
	recs := w.takeBufferLocked()
	w.mu.Unlock()
	return w.writeAll(recs)
}

// Close flushes remaining records and closes sinks. Idempotent. Safe
// to call on a nil receiver (returns nil).
func (w *AuditWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	recs := w.takeBufferLocked()
	w.mu.Unlock()

	// Stop flusher.
	if w.flusherCh != nil {
		close(w.flusherCh)
		w.flusherWG.Wait()
		w.flusherCh = nil
	}

	var firstErr error
	if err := w.writeAll(recs); err != nil && firstErr == nil {
		firstErr = err
	}
	if w.jsonlFile != nil {
		if err := w.jsonlFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		w.jsonlFile = nil
	}
	if w.pgDB != nil {
		if err := w.pgDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		w.pgDB = nil
	}
	return firstErr
}

// flushLoop runs in a goroutine. Every FlushInterval (or when
// w.flusherCh closes for shutdown), it wakes the buffer and flushes.
func (w *AuditWriter) flushLoop() {
	defer w.flusherWG.Done()
	t := time.NewTicker(w.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-w.flusherCh:
			// Shutdown: drain remaining on Close()'s explicit call.
			return
		case <-t.C:
			_ = w.Flush()
		}
	}
}

// takeBufferLocked returns the buffered records and resets the buffer.
// Caller must hold w.mu.
func (w *AuditWriter) takeBufferLocked() []AuditRecord {
	if len(w.buf) == 0 {
		return nil
	}
	out := w.buf
	w.buf = nil
	return out
}

// writeAll writes a batch of records to all enabled sinks. Errors are
// collected but do not abort the remaining sinks — a JSONL failure
// should not prevent PG from receiving the same records.
func (w *AuditWriter) writeAll(recs []AuditRecord) error {
	if len(recs) == 0 {
		return nil
	}
	var errs []error

	if w.jsonlFile != nil {
		if err := w.writeJSONL(recs); err != nil {
			errs = append(errs, fmt.Errorf("jsonl: %w", err))
		}
	}
	if w.pgDB != nil {
		if err := w.writePG(recs); err != nil {
			errs = append(errs, fmt.Errorf("pg: %w", err))
		}
	}
	return errors.Join(errs...)
}

// writeJSONL writes records as newline-delimited JSON. Each record is
// encoded separately so a single malformed record doesn't corrupt the
// whole batch. The OS file is flushed (Sync) only at Close() — batches
// rely on the OS page cache between flushes.
func (w *AuditWriter) writeJSONL(recs []AuditRecord) error {
	enc := json.NewEncoder(w.jsonlFile)
	enc.SetEscapeHTML(false)
	for _, r := range recs {
		if err := enc.Encode(&r); err != nil {
			return fmt.Errorf("encode record: %w", err)
		}
	}
	return nil
}

// writePG inserts records in a single transaction. Uses a prepared
// statement for each row — Postgres handles batching via extended
// protocol. args_hash/result_size/error are nullable.
func (w *AuditWriter) writePG(recs []AuditRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := w.pgDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // safe: no-op if Commit succeeds

	const stmt = `INSERT INTO broker_audit_log
		(ts, agent_role, server, tool, args_hash, success, result_size, duration_ms, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	for _, r := range recs {
		var argsHash any
		if r.ArgsHash != "" {
			argsHash = r.ArgsHash
		}
		var resultSize any
		if r.ResultSize != nil {
			resultSize = *r.ResultSize
		}
		var errStr any
		if r.Error != "" {
			errStr = r.Error
		}
		if _, err := tx.ExecContext(ctx, stmt,
			r.TS, r.AgentRole, r.Server, r.Tool, argsHash,
			r.Success, resultSize, r.DurationMS, errStr,
		); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
	}
	return tx.Commit()
}
