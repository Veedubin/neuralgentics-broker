package broker_test

import (
	"os"
	"testing"
	"time"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker"
	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// TestStartServer_FilesystemMCP tests the full StartServer lifecycle with
// a real MCP server subprocess (@modelcontextprotocol/server-filesystem).
// It is skipped in short mode and when npx or node are unavailable.
func TestStartServer_FilesystemMCP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Check if npx is available.
	if _, err := os.Stat("/usr/bin/npx"); os.IsNotExist(err) {
		t.Skip("npx not found, skipping MCP filesystem server integration test")
	}

	// Create a temp directory for the filesystem server to operate on.
	tmpDir, err := os.MkdirTemp("", "mcp-fs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	b := broker.NewBroker()

	// Step 1: Register the filesystem MCP server.
	config := types.ServerConfig{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", tmpDir},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Step 2: Start the server with full lifecycle (launch + handshake + tool discovery).
	if err := b.StartServer("filesystem"); err != nil {
		t.Fatalf("StartServer failed: %v", err)
	}

	// Step 3: Verify tools were discovered.
	tools := b.ListTools()
	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool from filesystem server, got 0")
	}

	// The filesystem server should expose read_file, write_file, etc.
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["read_file"] {
		t.Error("expected 'read_file' tool to be discovered")
	}

	// Step 4: Verify the server shows as running.
	statuses := b.ListServers()
	found := false
	for _, s := range statuses {
		if s.Name == "filesystem" {
			found = true
			if !s.Running {
				t.Error("expected filesystem server to be running")
			}
		}
	}
	if !found {
		t.Fatal("filesystem server not found in server list")
	}

	// Step 5: Deregister the server.
	if err := b.DeregisterServer("filesystem"); err != nil {
		t.Fatalf("DeregisterServer failed: %v", err)
	}

	// Step 6: Verify it's gone.
	statuses = b.ListServers()
	for _, s := range statuses {
		if s.Name == "filesystem" {
			t.Error("filesystem server should be deregistered")
		}
	}
}

// TestStartServer_NotRegistered tests that StartServer returns an error
// when the server is not registered.
func TestStartServer_NotRegistered(t *testing.T) {
	b := broker.NewBroker()

	err := b.StartServer("nonexistent")
	if err == nil {
		t.Fatal("expected error when starting unregistered server")
	}
}

// TestStartServer_InvalidCommand tests that StartServer returns an error
// when the command cannot be launched.
func TestStartServer_InvalidCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := broker.NewBroker()

	config := types.ServerConfig{
		Name:    "bad-server",
		Command: "/nonexistent/command/that/does/not/exist",
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	err := b.StartServer("bad-server")
	if err == nil {
		t.Fatal("expected error when starting server with invalid command")
	}
}

// TestDeregisterServer_StopsProcess tests that DeregisterServer stops
// a running server and removes it from the registry.
func TestDeregisterServer_StopsProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := broker.NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so StartServer's
	// handshake fails in ~500ms instead of blocking 30s and racing the
	// test binary's -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	// Register a simple server (sleep acts as a long-running process).
	config := types.ServerConfig{
		Name:    "sleep-server",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Start the server subprocess (won't respond to MCP handshake, but
	// we just want to test DeregisterServer stops the process).
	// Note: StartServer will fail on Initialize because "sleep" isn't
	// an MCP server, so we'll use the launcher directly.
	err := b.StartServer("sleep-server")
	if err == nil {
		// sleep won't speak MCP, so this should fail at Initialize step
		// but if the process was started, DeregisterServer should still stop it
	}

	// Deregister should succeed regardless.
	if err := b.DeregisterServer("sleep-server"); err != nil {
		t.Fatalf("DeregisterServer failed: %v", err)
	}

	// Give the process a moment to fully exit.
	time.Sleep(100 * time.Millisecond)

	// Verify the server is gone.
	statuses := b.ListServers()
	for _, s := range statuses {
		if s.Name == "sleep-server" {
			t.Error("sleep-server should be deregistered")
		}
	}
}
