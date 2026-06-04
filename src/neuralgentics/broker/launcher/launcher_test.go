package launcher

import (
	"os"
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
	entry.Process, _ = os.FindProcess(os.Getpid())
	reg.UpdateEntry("test-server", entry)

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
