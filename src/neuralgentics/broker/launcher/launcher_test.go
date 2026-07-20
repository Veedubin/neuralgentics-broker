package launcher

import (
	"os"
	"strings"
	"testing"

	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

func TestHealth_NotRegistered(t *testing.T) {
	reg := registry.NewRegistry()
	l := NewLauncher(reg)

	status := l.Health("nonexistent")
	if status != HealthStopped {
		t.Errorf("expected HealthStopped for nonexistent server, got %q", status)
	}
}

func TestHealth_NoProcess(t *testing.T) {
	reg := registry.NewRegistry()
	l := NewLauncher(reg)

	cfg := types.ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Type:    "stdio",
	}
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	status := l.Health("test-server")
	if status != HealthStopped {
		t.Errorf("expected HealthStopped for server with no process, got %q", status)
	}
}

func TestHealth_Initializing(t *testing.T) {
	// Simulate a process that's alive but has no pipes (initializing state).
	// os.FindProcess on Unix always succeeds; Signal(0) on the
	// current PID must succeed (process exists, pipes are nil → initializing).
	reg := registry.NewRegistry()
	l := NewLauncher(reg)

	cfg := types.ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Type:    "stdio",
	}
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	entry, _ := reg.Get("test-server")
	// Lock around the mutation so a concurrent Health caller cannot
	// observe a torn tuple.
	entry.Lock()
	entry.Process, _ = os.FindProcess(os.Getpid())
	entry.Unlock()

	status := l.Health("test-server")
	if status != HealthInitializing {
		t.Errorf("expected HealthInitializing for alive process with no pipes, got %q", status)
	}
}

func TestHealthStatus_Constants(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{HealthStopped, "stopped"},
		{HealthDead, "dead"},
		{HealthInitializing, "initializing"},
		{HealthHealthy, "healthy"},
		{HealthUnhealthy, "unhealthy"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, tt.status)
		}
	}
}

// ─── BuildCommandForTransport Tests (T-TRANSPORT-ABSTRACTION) ────────────────

func TestBuildCommandForTransport_NPX(t *testing.T) {
	t.Parallel()

	tc := types.TransportConfig{
		Type:    types.TransportNPX,
		Package: "@modelcontextprotocol/server-github",
		Env:     map[string]string{"GITHUB_TOKEN": "test"},
	}
	cmd, _, _, err := BuildCommandForTransport(tc, "github-mcp", "GitHub", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Path != "" && !strings.HasSuffix(cmd.Path, "npx") {
		// cmd.Path might be the resolved path or just the command name
		t.Errorf("command path: got %q, want to end with npx", cmd.Path)
	}
	if len(cmd.Args) < 3 {
		t.Fatalf("args: got %d args, want at least 3", len(cmd.Args))
	}
	// cmd.Args includes the command itself as args[0]
	foundPackage := false
	for _, arg := range cmd.Args {
		if arg == "@modelcontextprotocol/server-github" {
			foundPackage = true
			break
		}
	}
	if !foundPackage {
		t.Errorf("expected package name in args, got %v", cmd.Args)
	}
}

func TestBuildCommandForTransport_Docker(t *testing.T) {
	t.Parallel()

	tc := types.TransportConfig{
		Type:    types.TransportDocker,
		Package: "ghcr.io/example/mcp-server:v1",
	}
	cmd, _, _, err := BuildCommandForTransport(tc, "docker-mcp", "Docker", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Command should be docker or podman
	if cmd == nil {
		t.Fatal("expected non-nil cmd for docker transport")
	}
	// Args should contain "run", "-i", "--rm", and the image
	foundImage := false
	for _, arg := range cmd.Args {
		if arg == "ghcr.io/example/mcp-server:v1" {
			foundImage = true
			break
		}
	}
	if !foundImage {
		t.Errorf("expected docker image in args, got %v", cmd.Args)
	}
}

func TestBuildCommandForTransport_HTTP(t *testing.T) {
	t.Parallel()

	tc := types.TransportConfig{
		Type: types.TransportHTTP,
		URL:  "https://mcp.example.com/api",
	}
	cmd, stdinPipe, stdoutPipe, err := BuildCommandForTransport(tc, "http-mcp", "HTTP MCP", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// HTTP transport creates a cmd with empty Command but no pipes
	// (the proxy layer handles HTTP/SSE connections)
	if cmd == nil {
		t.Fatal("expected non-nil cmd from buildCommand, got nil")
	}
	// HTTP transport should have no pipes set up
	if stdinPipe != nil {
		t.Errorf("expected nil stdinPipe for http transport, got non-nil")
	}
	if stdoutPipe != nil {
		t.Errorf("expected nil stdoutPipe for http transport, got non-nil")
	}
}
