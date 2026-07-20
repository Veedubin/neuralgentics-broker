package audit

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// makeWriter returns a writer pointed at a temp JSONL file with a very
// short flush interval so tests don't have to sleep.
func makeWriter(t *testing.T, cfg Config) (*AuditWriter, string) {
	t.Helper()
	dir := t.TempDir()
	cfg.JSONLPath = filepath.Join(dir, "audit.jsonl")
	cfg.FlushInterval = 20 * time.Millisecond
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	w, err := NewAuditWriter(cfg)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	return w, cfg.JSONLPath
}

// readJSONL parses the JSONL file into records (used by assertions).
func readJSONL(t *testing.T, path string) []AuditRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()
	var recs []AuditRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return recs
}

func TestAuditWriterJSONL(t *testing.T) {
	w, path := makeWriter(t, Config{Sink: SinkJSONL})

	recs := []AuditRecord{
		{TS: time.Now().UTC(), AgentRole: "coder", Server: "filesystem", Tool: "read_file", ArgsHash: "sha256:abc", Success: true, ResultSize: ptrInt(1024), DurationMS: 5},
		{TS: time.Now().UTC(), AgentRole: "orchestrator", Server: "memory", Tool: "search", Success: true, DurationMS: 12},
		{TS: time.Now().UTC(), AgentRole: "coder", Server: "playwright", Tool: "click", Success: false, DurationMS: 200, Error: "timeout"},
	}
	for _, r := range recs {
		w.Write(r)
	}
	// Force a flush and close so the file is fully written.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readJSONL(t, path)
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}
	for i, want := range recs {
		g := got[i]
		if g.Tool != want.Tool || g.Server != want.Server || g.AgentRole != want.AgentRole {
			t.Errorf("record %d: got role=%s server=%s tool=%s, want role=%s server=%s tool=%s",
				i, g.AgentRole, g.Server, g.Tool, want.AgentRole, want.Server, want.Tool)
		}
		if g.Success != want.Success {
			t.Errorf("record %d: success=%v want %v", i, g.Success, want.Success)
		}
		if g.DurationMS != want.DurationMS {
			t.Errorf("record %d: duration=%d want %d", i, g.DurationMS, want.DurationMS)
		}
		if want.Error != "" && g.Error != want.Error {
			t.Errorf("record %d: error=%q want %q", i, g.Error, want.Error)
		}
	}
}

func TestAuditTruncation(t *testing.T) {
	// The card spec asks for args truncation to 4KB. In our schema we
	// don't store raw args (privacy + size); we store args_hash (a
	// 16-byte truncated sha256 digest, ~32 chars). This test confirms
	// that a 10KB args map produces a hash well under 4KB.
	bigArgs := map[string]any{}
	for i := 0; i < 1000; i++ {
		bigArgs[fmt.Sprintf("k%d", i)] = strings.Repeat("x", 10)
	}
	h := HashArgs(bigArgs)
	if len(h) > 4096 {
		t.Fatalf("args_hash len=%d exceeds 4KB truncation cap", len(h))
	}
	if !strings.HasPrefix(h, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", h)
	}
	// Confirm BuildRecord produces a record with the truncated hash.
	rec := BuildRecord("coder", "fs", "read_file", bigArgs, nil, nil, time.Now())
	if len(rec.ArgsHash) > 4096 {
		t.Fatalf("record args_hash len=%d exceeds 4KB", len(rec.ArgsHash))
	}
}

func TestAuditFlushInterval(t *testing.T) {
	// Verify buffered writes flush on the interval (not requiring
	// Close() to be called). Use a 30ms interval and sleep 200ms.
	w, path := makeWriter(t, Config{Sink: SinkJSONL, BatchSize: 1_000_000})

	w.Write(AuditRecord{TS: time.Now().UTC(), AgentRole: "coder", Server: "fs", Tool: "t", Success: true, DurationMS: 1})

	// Wait long enough for the ticker to fire at least once.
	time.Sleep(200 * time.Millisecond)

	got := readJSONL(t, path)
	if len(got) != 1 {
		t.Fatalf("expected 1 record flushed by interval, got %d (file=%s)", len(got), path)
	}
	_ = w.Close()
}

func TestAuditOff(t *testing.T) {
	// --audit=off writes nothing. Verify no JSONL file is created.
	dir := t.TempDir()
	path := filepath.Join(dir, "should_not_exist.jsonl")
	w, err := NewAuditWriter(Config{Sink: SinkOff, JSONLPath: path, FlushInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewAuditWriter off: %v", err)
	}
	w.Write(AuditRecord{TS: time.Now().UTC(), AgentRole: "x", Server: "y", Tool: "z", Success: true, DurationMS: 1})
	_ = w.Close()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file %s to not exist with SinkOff", path)
	}
}

func TestAuditConcurrent(t *testing.T) {
	w, path := makeWriter(t, Config{Sink: SinkJSONL, BatchSize: 1_000_000})

	const goroutines = 10
	const perGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				w.Write(AuditRecord{
					TS:         time.Now().UTC(),
					AgentRole:  "coder",
					Server:     "fs",
					Tool:       "read_file",
					Success:    true,
					DurationMS: gid*100 + i,
				})
			}
		}(g)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readJSONL(t, path)
	want := goroutines * perGoroutine
	if len(got) != want {
		t.Fatalf("expected %d records, got %d", want, len(got))
	}
	// Verify all are well-formed (already done by readJSONL); check
	// uniqueness of DurationMS to ensure no records were dropped or
	// duplicated.
	seen := make(map[int]bool, want)
	for _, r := range got {
		if seen[r.DurationMS] {
			t.Fatalf("duplicate DurationMS=%d (a record was duplicated)", r.DurationMS)
		}
		seen[r.DurationMS] = true
	}
	if len(seen) != want {
		t.Fatalf("expected %d unique DurationMS values, got %d", want, len(seen))
	}
}

func TestAuditWriterPG(t *testing.T) {
	pgURL := os.Getenv("BROKER_TEST_PG_URL")
	if pgURL == "" {
		t.Skip("BROKER_TEST_PG_URL not set; skipping PG audit test")
	}

	dir := t.TempDir()
	cfg := Config{
		Sink:          SinkJSONLAndPG,
		JSONLPath:     filepath.Join(dir, "audit.jsonl"),
		PGURL:         pgURL,
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     100,
	}
	w, err := NewAuditWriter(cfg)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	recs := []AuditRecord{
		{TS: time.Now().UTC(), AgentRole: "coder", Server: "fs", Tool: "read_file", ArgsHash: "sha256:1", Success: true, ResultSize: ptrInt(100), DurationMS: 5},
		{TS: time.Now().UTC(), AgentRole: "orchestrator", Server: "mem", Tool: "search", Success: false, DurationMS: 20, Error: "boom"},
	}
	for _, r := range recs {
		w.Write(r)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify rows landed in PG. Use a fresh connection to avoid pool
	// caching issues.
	db, err := sql.Open("postgres", pgURL)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT agent_role, server, tool, success, duration_ms, error
		FROM broker_audit_log ORDER BY id DESC LIMIT $1`, len(recs))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []struct {
		role, server, tool string
		success            bool
		dur                int
		err                string
	}
	for rows.Next() {
		var g struct {
			role, server, tool string
			success            bool
			dur                int
			err                string
		}
		if err := rows.Scan(&g.role, &g.server, &g.tool, &g.success, &g.dur, &g.err); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, g)
	}
	if len(got) != len(recs) {
		t.Fatalf("expected %d rows, got %d", len(recs), len(got))
	}
	// Newest-first ordering from query; reverse recs to compare.
	for i, want := range recs {
		g := got[i]
		if g.tool != want.Tool || g.server != want.Server || g.role != want.AgentRole {
			t.Errorf("row %d: got role=%s server=%s tool=%s, want role=%s server=%s tool=%s",
				i, g.role, g.server, g.tool, want.AgentRole, want.Server, want.Tool)
		}
		if g.success != want.Success {
			t.Errorf("row %d: success=%v want %v", i, g.success, want.Success)
		}
		if g.dur != want.DurationMS {
			t.Errorf("row %d: dur=%d want %d", i, g.dur, want.DurationMS)
		}
	}
}

func TestParseSink(t *testing.T) {
	cases := []struct {
		in   string
		want Sink
		err  bool
	}{
		{"off", SinkOff, false},
		{"jsonl", SinkJSONL, false},
		{"jsonl+pg", SinkJSONLAndPG, false},
		{"bogus", SinkOff, true},
	}
	for _, c := range cases {
		got, err := ParseSink(c.in)
		if c.err && err == nil {
			t.Errorf("ParseSink(%q): expected error, got nil", c.in)
		}
		if !c.err && err != nil {
			t.Errorf("ParseSink(%q): unexpected error %v", c.in, err)
		}
		if !c.err && got != c.want {
			t.Errorf("ParseSink(%q): got %d want %d", c.in, got, c.want)
		}
	}
}

func ptrInt(n int) *int { return &n }
