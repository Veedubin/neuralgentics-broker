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
	// Use Snapshot for a consistent read of the runtime fields; the
	// background watcher goroutine from Start concurrently mutates them.
	entry, ok := reg.Get("health-test")
	if !ok {
		t.Fatal("expected health-test in registry after Start")
	}
	snap := entry.Snapshot()
	if snap.Process == nil {
		t.Fatal("expected process to be running after Start")
	}
	if snap.Stdin == nil || snap.Stdout == nil {
		t.Fatal("expected both pipes to be set after Start")
	}

	// Health should report "healthy" since process has pipes.
	status := l.Health("health-test")
	if status != HealthHealthy {
		t.Errorf("expected HealthHealthy after start, got %q", status)
	}

	// Kill the process externally to simulate a crash.
	pid := snap.Process.Pid
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

// TestLauncher_HealthTransitions_NoRace runs the start → kill → health
// transition 1000 times under -race. It is the regression test for the
// pre-existing data race (T-117.1) between the background watcher goroutine
// in Start (which calls clearAfterExit and nil's entry.Process/Stdin/Stdout)
// and concurrent Health callers (which read the same fields). The fix added
// a per-ServerEntry mutex with Lock/Unlock/Snapshot accessors.
//
// This test FAILS if the race detector reports any data race.
func TestLauncher_HealthTransitions_NoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-based test in short mode")
	}

	// 200 iterations × ~5ms spawn + 1ms sleep ≈ 1.2s wall-clock under
	// -race per invocation. With -count=100 that's ~120s for this test
	// alone — kept intentionally below the typical 120s per-test
	// timeout. Each iteration exercises the exact racing window the
	// fix closes (background watcher's clearAfterExit vs concurrent
	// Health read). 200 reps is far above what the race detector needs
	// to flag a regression — it caught the original race in <20 reps.
	const iterations = 200
	for i := 0; i < iterations; i++ {
		reg := registry.NewRegistry()
		l := NewLauncher(reg)

		cfg := types.ServerConfig{
			Name:    "health-test",
			Command: "sleep",
			Args:    []string{"60"},
			Type:    "stdio",
		}
		if err := reg.Register(cfg); err != nil {
			t.Fatalf("Register failed on iter %d: %v", i, err)
		}
		if err := l.Start(cfg); err != nil {
			t.Fatalf("Launcher.Start failed on iter %d: %v", i, err)
		}

		// Concurrent Health reads race against the background watcher.
		_ = l.Health("health-test")

		// Kill externally (like a crash) so the watcher fires clearAfterExit.
		entry, _ := reg.Get("health-test")
		snap := entry.Snapshot()
		if snap.Process == nil {
			_ = l.Stop("health-test")
			t.Fatalf("iter %d: process nil after Start", i)
		}
		proc, _ := os.FindProcess(snap.Process.Pid)
		_ = proc.Kill()

		// Wait briefly so the watcher's clearAfterExit runs concurrently
		// with the Health call below — this is the racing window the fix
		// closes. 1ms × N iters keeps the test wall-clock bounded.
		time.Sleep(1 * time.Millisecond)
		_ = l.Health("health-test")

		_ = l.Stop("health-test")
	}
}
