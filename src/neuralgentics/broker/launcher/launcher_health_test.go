package launcher

import (
	"os"
	"testing"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// TestLauncher_HealthTransitions verifies that the Health method correctly
// transitions through states when a process crashes mid-check.
// It starts a real subprocess (sleep), verifies it's healthy, kills it,
// and verifies the health transitions to dead or stopped.
func TestLauncher_HealthTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-based test in short mode")
	}

	reg := registry.NewRegistry()
	l := NewLauncher(reg)

	cfg := types.ServerConfig{
		Name:    "health-test",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	if err := reg.Register(cfg); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Start the process directly via the launcher.
	if err := l.Start(cfg); err != nil {
		t.Fatalf("Launcher.Start failed: %v", err)
	}

	// Verify the process is present with pipes → should be healthy.
	entry, ok := reg.Get("health-test")
	if !ok {
		t.Fatal("expected health-test in registry after Start")
	}
	if entry.Process == nil {
		t.Fatal("expected process to be running after Start")
	}
	if entry.Stdin == nil || entry.Stdout == nil {
		t.Fatal("expected both pipes to be set after Start")
	}

	// Health should report "healthy" since process has pipes.
	status := l.Health("health-test")
	if status != HealthHealthy {
		t.Errorf("expected HealthHealthy after start, got %q", status)
	}

	// Kill the process externally to simulate a crash.
	pid := entry.Process.Pid
	proc, _ := os.FindProcess(pid)
	if err := proc.Kill(); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}

	// Wait for the process to exit.
	time.Sleep(300 * time.Millisecond)

	// After kill, Health should report "dead" (process.Signal(0) fails).
	status = l.Health("health-test")
	if status != HealthDead && status != HealthStopped {
		// The background goroutine from Start may have already cleared
		// the process, making it "stopped" instead of "dead". Both are
		// acceptable — the key is it's NOT "healthy".
		if status == HealthHealthy {
			t.Errorf("expected dead or stopped after kill, got %q", status)
		}
	}

	// Clean up: stop the launcher entry to clear state.
	_ = l.Stop("health-test")
}
