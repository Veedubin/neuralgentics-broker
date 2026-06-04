package registry

import (
	"testing"

	"neuralgentics-broker/src/neuralgentics/broker/types"
)

func TestRegistry_UpdateEntry(t *testing.T) {
	reg := NewRegistry()

	cfg := types.ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Type:    "stdio",
	}
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify initial state: no tools, no process.
	entry, ok := reg.Get("test-server")
	if !ok {
		t.Fatal("expected test-server in registry after Register")
	}
	if len(entry.Tools) != 0 {
		t.Errorf("expected 0 tools initially, got %d", len(entry.Tools))
	}
	if entry.Process != nil {
		t.Error("expected nil Process initially")
	}

	// Update the entry with tools and verify.
	updatedTools := []types.ToolSummary{
		{Server: "test-server", Name: "tool_a", Description: "Tool A"},
		{Server: "test-server", Name: "tool_b", Description: "Tool B"},
	}
	entry.Tools = updatedTools
	reg.UpdateEntry("test-server", entry)

	// Fetch again to verify the update took effect.
	updated, ok := reg.Get("test-server")
	if !ok {
		t.Fatal("expected test-server in registry after UpdateEntry")
	}
	if len(updated.Tools) != 2 {
		t.Fatalf("expected 2 tools after update, got %d", len(updated.Tools))
	}
	if updated.Tools[0].Name != "tool_a" {
		t.Errorf("expected first tool 'tool_a', got %q", updated.Tools[0].Name)
	}
	if updated.Tools[1].Name != "tool_b" {
		t.Errorf("expected second tool 'tool_b', got %q", updated.Tools[1].Name)
	}
}

func TestRegistry_InferCapabilities_WithUnderscore(t *testing.T) {
	tools := []types.ToolSummary{
		{Name: "memory_add"},
		{Name: "memory_query"},
		{Name: "memory_delete"},
		{Name: "filesystem_read"},
		{Name: "ping"},
	}

	caps := InferCapabilities(tools)

	// Should extract "memory", "filesystem", and "ping" (deduplicated).
	capMap := make(map[string]bool)
	for _, c := range caps {
		capMap[c] = true
	}
	if !capMap["memory"] {
		t.Error("expected 'memory' capability")
	}
	if !capMap["filesystem"] {
		t.Error("expected 'filesystem' capability")
	}
	if !capMap["ping"] {
		t.Error("expected 'ping' capability")
	}
	// "memory" should appear only once despite 3 tools with that prefix.
	count := 0
	for _, c := range caps {
		if c == "memory" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 'memory' to appear once (dedup), got %d", count)
	}
	// Total unique capabilities = 3.
	if len(caps) != 3 {
		t.Errorf("expected 3 capabilities, got %d: %v", len(caps), caps)
	}
}

func TestRegistry_InferCapabilities_EmptyTools(t *testing.T) {
	caps := InferCapabilities(nil)
	if len(caps) != 0 {
		t.Errorf("expected 0 capabilities for nil input, got %d: %v", len(caps), caps)
	}

	caps = InferCapabilities([]types.ToolSummary{})
	if len(caps) != 0 {
		t.Errorf("expected 0 capabilities for empty input, got %d: %v", len(caps), caps)
	}
}
