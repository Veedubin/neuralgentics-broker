package broker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/audit"
	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// TestBrokerCallEmitsAudit verifies that Broker.Call wraps tool calls
// with audit records. We use a no-pipe (not-running) server so Call
// errors fast — the audit record still captures the (failed) call.
func TestBrokerCallEmitsAudit(t *testing.T) {
	dir := t.TempDir()
	cfg := audit.Config{
		Sink:          audit.SinkJSONL,
		JSONLPath:     filepath.Join(dir, "audit.jsonl"),
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     100,
	}
	w, err := audit.NewAuditWriter(cfg)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	b := NewBrokerWithWorkspace(dir)
	b.SetAuditWriter(w)
	if err := b.RegisterServer(types.ServerConfig{Name: "fs", Command: "echo", Type: "stdio"}); err != nil {
		t.Fatalf("RegisterServer: %v", err)
	}

	// Call a server that's registered but not running → error path.
	_, _ = b.Call("coder", "fs", "read_file", map[string]any{"path": "/tmp"})

	// Close so the JSONL is fully written (defer also Close()s but that
	// is now idempotent and safe).
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected audit jsonl to contain at least one record")
	}
	// Sanity: the line should mention the tool and role.
	if !contains(string(data), "read_file") || !contains(string(data), "coder") {
		t.Errorf("audit record missing expected fields: %s", string(data))
	}
}

// TestBrokerCallDeniedEmitsAudit verifies that an access-denied call
// is still audited (with success=false and the ErrUnauthorized text).
func TestBrokerCallDeniedEmitsAudit(t *testing.T) {
	dir := t.TempDir()
	cfg := audit.Config{
		Sink:          audit.SinkJSONL,
		JSONLPath:     filepath.Join(dir, "audit.jsonl"),
		FlushInterval: 20 * time.Millisecond,
		BatchSize:     100,
	}
	w, err := audit.NewAuditWriter(cfg)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	b := NewBrokerWithWorkspace(dir)
	b.SetAuditWriter(w)
	// Register a server the "coder" role can't access (playwright by
	// default access policy).
	if err := b.RegisterServer(types.ServerConfig{Name: "playwright", Command: "echo", Type: "stdio"}); err != nil {
		t.Fatalf("RegisterServer: %v", err)
	}

	_, callErr := b.Call("coder", "playwright", "click", nil)
	if callErr == nil {
		t.Fatal("expected access-denied error")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	if !contains(string(data), "playwright") {
		t.Errorf("audit record missing server name: %s", string(data))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
