package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// HashArgs returns a "sha256:<hex>" digest of the tool arguments, or
// "" if args is nil. We do NOT store raw args (privacy + size) — the
// consumer (T-107) reads args_hash, not arguments.
func HashArgs(args map[string]any) string {
	if args == nil {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		// Non-deterministic marshal shouldn't happen for map[string]any,
		// but degrade gracefully rather than fail the audit path.
		return "sha256:marshal-error"
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:16])
}

// ResultSize returns the byte length of a JSON-encoded tool result, or
// nil if result is nil. Used to populate result_size without storing
// the result itself.
func ResultSize(result map[string]any) *int {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	n := len(data)
	return &n
}

// BuildRecord constructs an AuditRecord from a tool-call outcome. The
// caller supplies the start time; duration is computed against now.
func BuildRecord(role, server, tool string, args map[string]any, result map[string]any, callErr error, start time.Time) AuditRecord {
	dur := time.Since(start)
	rec := AuditRecord{
		TS:         start.UTC(),
		AgentRole:  role,
		Server:     server,
		Tool:       tool,
		ArgsHash:   HashArgs(args),
		Success:    callErr == nil,
		ResultSize: ResultSize(result),
		DurationMS: int(dur.Milliseconds()),
	}
	if callErr != nil {
		rec.Error = callErr.Error()
	}
	return rec
}

// String returns a human-readable audit config summary for logs.
func (c Config) String() string {
	sink := "off"
	switch c.Sink {
	case SinkJSONL:
		sink = "jsonl"
	case SinkPG:
		sink = "pg"
	case SinkJSONLAndPG:
		sink = "jsonl+pg"
	}
	return fmt.Sprintf("audit{sink=%s jsonl=%s pg=%v flush=%s batch=%d}",
		sink, c.JSONLPath, c.PGURL != "", c.FlushInterval, c.BatchSize)
}
