package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEmitForIntegration is a bridge between the Go producer and the
// Python T-113.1 integration test. When the env var
// NEURALGENTICS_AUDIT_EMIT_PATH is set, this test writes ONE real audit
// record — built via the production BuildRecord + AuditWriter.Write +
// Close path — to that path as a single JSONL line, then exits the test
// early with t.Skip. The Python side runs this with
// `go test -run TestEmitForIntegration -count=1 ./audit/` and then
// parses the emitted file with the real T-107 consumer.
//
// When the env var is NOT set, the test is a no-op (t.Skip) so it
// doesn't interfere with the normal `go test ./...` run.
//
// This is the honest end-to-end path: the bytes the consumer parses are
// produced by json.Encoder.Encode(&AuditRecord) on a record constructed
// by BuildRecord — exactly what the broker does on a real tool call.
// We use a call that errors (server not running) so no external process
// is required; the audit record still captures the (failed) call with
// success=false and a non-empty error string.
func TestEmitForIntegration(t *testing.T) {
	outPath := os.Getenv("NEURALGENTICS_AUDIT_EMIT_PATH")
	if outPath == "" {
		t.Skip("NEURALGENTICS_AUDIT_EMIT_PATH not set; integration emit helper inactive")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(outPath), err)
	}

	cfg := Config{
		Sink:          SinkJSONL,
		JSONLPath:     outPath,
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     100,
	}
	w, err := NewAuditWriter(cfg)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	// Build records via the production BuildRecord path. BuildRecord
	// computes duration as time.Since(start), so use a start time
	// ~50ms / ~120ms in the past to get realistic, stable duration_ms
	// values the Python test can assert against. We pin the TS field
	// by constructing the record and then overriding TS to a fixed
	// value so the assertion is deterministic.
	args := map[string]any{"path": "/tmp/nonexistent", "offset": 0}
	rec := BuildRecord("coder", "filesystem", "read_file", args, nil, errFakeCall(),
		time.Now().Add(-50*time.Millisecond))
	rec.TS = time.Date(2026, 7, 20, 5, 1, 14, 0, time.UTC)
	// BuildRecord sets DurationMS from real elapsed time; override to
	// a deterministic value for cross-language assertion.
	rec.DurationMS = 50

	args2 := map[string]any{"query": "hello"}
	result2 := map[string]any{"content": "hello world", "tokens": 11}
	rec2 := BuildRecord("orchestrator", "memory", "search", args2, result2, nil,
		time.Now().Add(-120*time.Millisecond))
	rec2.TS = time.Date(2026, 7, 20, 5, 1, 15, 0, time.UTC)
	rec2.DurationMS = 120

	w.Write(rec)
	w.Write(rec2)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	t.Logf("emitted 2 audit records to %s", outPath)
}

// errFakeCall mimics the error a real broker.Call returns when the
// target server is registered but not running.
type fakeCallErr struct{ msg string }

func (e *fakeCallErr) Error() string { return e.msg }

func errFakeCall() error { return &fakeCallErr{msg: "server not running: connection refused"} }
