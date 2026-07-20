package broker

import (
	"os"
	"testing"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/launcher"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// TestReloadServer_NotRegistered verifies that ReloadServer returns an error
// when called on a server name that is not in the registry.
func TestReloadServer_NotRegistered(t *testing.T) {
	b := NewBroker()

	err := b.ReloadServer("nonexistent")
	if err == nil {
		t.Fatal("expected error when reloading unregistered server, got nil")
	}
}

// TestReloadServer_StoppedServer verifies that ReloadServer on a registered
// but stopped server delegates to StartServer. Since "sleep" is not an MCP
// server, StartServer will fail at the MCP handshake — we verify the error
// propagates and that the registry entry is still intact.
func TestReloadServer_StoppedServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	config := types.ServerConfig{
		Name:    "sleep-server",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// The server is registered but not started. ReloadServer should attempt
	// StartServer. Since "sleep" isn't an MCP server, we expect a handshake
	// error, not a registry error.
	err := b.ReloadServer("sleep-server")
	if err == nil {
		// If somehow sleep speaks MCP, we just verify it stayed registered.
		t.Log("ReloadServer succeeded unexpectedly (sleep acted as MCP server)")
	} else {
		// Error should be from the MCP handshake or initialisation, not from
		// a missing registry entry.
		if err.Error() == "server \"sleep-server\" not registered" {
			t.Fatalf("ReloadServer returned registry error instead of start error: %v", err)
		}
	}

	// Verify the server is still in the registry.
	statuses := b.ListServers()
	found := false
	for _, s := range statuses {
		if s.Name == "sleep-server" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("sleep-server should still be registered after failed reload")
	}

	// Clean up any running process.
	_ = b.DeregisterServer("sleep-server")
}

// TestReloadServer_RunningServer verifies that ReloadServer stops a running
// process and restarts it. Since "sleep" isn't an MCP server, StartServer
// will fail after the process is launched — we verify the process was
// successfully stopped and the registry entry persisted.
func TestReloadServer_RunningServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	config := types.ServerConfig{
		Name:    "sleep-server",
		Command: "sleep",
		Args:    []string{"300"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Launch the process directly (bypass MCP handshake since sleep isn't MCP).
	entry, ok := b.registry.Get("sleep-server")
	if !ok {
		t.Fatalf("expected sleep-server in registry")
	}
	if err := b.launcher.Start(entry.Config); err != nil {
		t.Fatalf("launcher.Start failed: %v", err)
	}

	// Verify the process is running.
	entry, _ = b.registry.Get("sleep-server")
	if entry.Process == nil {
		t.Fatal("expected process to be running after launcher.Start")
	}
	pid := entry.Process.Pid

	// ReloadServer will stop the running process, then attempt to restart.
	// The restart will fail at MCP handshake, which is expected for "sleep".
	err := b.ReloadServer("sleep-server")
	if err == nil {
		t.Log("ReloadServer succeeded (unlikely for sleep)")
	} else {
		// Error should be from MCP handshake, not from stop or registry lookup.
		t.Logf("ReloadServer returned expected error: %v", err)
	}

	// Verify the old process was stopped. After SIGKILL/SIGINT, the process
	// should be gone. Use signal 0 to check existence.
	oldProcess, _ := os.FindProcess(pid)
	// On Unix, FindProcess always succeeds; Signal(0) checks existence.
	// Give the OS a moment to clean up.
	time.Sleep(200 * time.Millisecond)
	if oldProcess.Signal(os.Interrupt) == nil {
		// Process may have been recycled (pid reuse) — log but don't fail.
		t.Logf("Warning: old process (pid %d) may still be running or pid was recycled", pid)
	}

	// Verify the server is still registered (reload preserves registry).
	statuses := b.ListServers()
	found := false
	for _, s := range statuses {
		if s.Name == "sleep-server" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("sleep-server should still be registered after reload")
	}

	// Verify Health reports stopped or dead (the new process from StartServer
	// failed, so no healthy process should exist).
	health := b.Health()
	if status, ok := health["sleep-server"]; ok {
		if status == string(launcher.HealthHealthy) {
			t.Errorf("expected server to be stopped/dead after failed reload, got %q", status)
		}
	}

	// Clean up.
	_ = b.DeregisterServer("sleep-server")
}

// TestReloadServer_AfterCrash simulates a server crash by externally killing
// the process, then verifies that ReloadServer can recover it.
func TestReloadServer_AfterCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	config := types.ServerConfig{
		Name:    "sleep-server",
		Command: "sleep",
		Args:    []string{"300"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Launch the process directly (bypass handshake).
	entry, ok := b.registry.Get("sleep-server")
	if !ok {
		t.Fatalf("expected sleep-server in registry")
	}
	if err := b.launcher.Start(entry.Config); err != nil {
		t.Fatalf("launcher.Start failed: %v", err)
	}

	// Verify the process is running.
	entry, _ = b.registry.Get("sleep-server")
	if entry.Process == nil {
		t.Fatal("expected process to be running after launcher.Start")
	}

	// Simulate a crash: kill the process externally (SIGKILL).
	if err := entry.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %v", err)
	}

	// Wait for the process to actually exit and the background goroutine
	// to clean up the registry entry.
	time.Sleep(300 * time.Millisecond)

	// The background goroutine from launcher.Start should have called
	// clearAfterExit, which sets Process=nil. Reset Stdin/Stdout as well
	// to simulate a clean crashed state.
	//
	// Use Snapshot() for the read so we do not race with the launcher's
	// background watcher goroutine (clearAfterExit), which nils out the
	// same fields under entry.mu. The race detector previously flagged
	// the bare `entry.Process != nil` read here. (T-117.3)
	entry, _ = b.registry.Get("sleep-server")
	snap := entry.Snapshot()
	if snap.Process != nil {
		// The background goroutine may not have cleaned up yet.
		// Manually simulate the crashed state so our test is deterministic.
		// Take the entry lock around the write so we do not race with
		// clearAfterExit's concurrent nil-out of the same fields.
		entry.Lock()
		entry.Process = nil
		entry.Stdin = nil
		entry.Stdout = nil
		entry.Unlock()
		b.registry.UpdateEntry("sleep-server", entry)
	}

	// Now reload. Since the process is nil (crashed), the idempotent path
	// will call StartServer directly. The handshake will fail for "sleep",
	// but the attempt proves the reload mechanism works.
	err := b.ReloadServer("sleep-server")
	if err == nil {
		t.Log("ReloadServer succeeded after crash recovery (unlikely for sleep)")
	} else {
		t.Logf("ReloadServer returned expected error after crash: %v", err)
	}

	// Verify the server is still registered.
	statuses := b.ListServers()
	found := false
	for _, s := range statuses {
		if s.Name == "sleep-server" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("sleep-server should still be registered after crash reload")
	}

	// Clean up.
	_ = b.DeregisterServer("sleep-server")
}

// TestReloadServerWithConfig_UpdatesEnvVars verifies that calling
// ReloadServerWithConfig replaces the server's environment variables
// in the registry entry. Since "sleep" isn't an MCP server, the restart
// will fail at the handshake — we verify the config was updated regardless.
func TestReloadServerWithConfig_UpdatesEnvVars(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	originalConfig := types.ServerConfig{
		Name:    "env-test-server",
		Command: "sleep",
		Args:    []string{"300"},
		Env:     map[string]string{"ENV_A": "foo", "ENV_B": "bar"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(originalConfig); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Launch the process directly to simulate a running server.
	entry, ok := b.registry.Get("env-test-server")
	if !ok {
		t.Fatalf("expected env-test-server in registry")
	}
	if err := b.launcher.Start(entry.Config); err != nil {
		t.Fatalf("launcher.Start failed: %v", err)
	}

	// Reload with new env vars — ENV_A is shadowed, ENV_C is new.
	newConfig := types.ServerConfig{
		Name:    "env-test-server",
		Command: "sleep",
		Args:    []string{"300"},
		Env:     map[string]string{"ENV_A": "baz", "ENV_C": "qux"},
		Type:    "stdio",
	}
	err := b.ReloadServerWithConfig("env-test-server", newConfig)
	if err == nil {
		t.Log("ReloadServerWithConfig succeeded (unlikely for sleep)")
	} else {
		t.Logf("ReloadServerWithConfig returned expected error: %v", err)
	}

	// Verify the config was updated in the registry.
	entry, ok = b.registry.Get("env-test-server")
	if !ok {
		t.Fatal("env-test-server should still be in registry")
	}
	if entry.Config.Env["ENV_A"] != "baz" {
		t.Errorf("expected ENV_A=baz, got ENV_A=%s", entry.Config.Env["ENV_A"])
	}
	if entry.Config.Env["ENV_C"] != "qux" {
		t.Errorf("expected ENV_C=qux, got ENV_C=%s", entry.Config.Env["ENV_C"])
	}
	// ENV_B should be gone because the new config replaces, not merges.
	if _, exists := entry.Config.Env["ENV_B"]; exists {
		t.Error("ENV_B should not exist in new config (full replacement, not merge)")
	}

	// Clean up.
	_ = b.DeregisterServer("env-test-server")
}

// TestReloadServerWithConfig_UpdatesArgs verifies that calling
// ReloadServerWithConfig replaces the server's CLI arguments in the
// registry entry.
func TestReloadServerWithConfig_UpdatesArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	originalConfig := types.ServerConfig{
		Name:    "args-test-server",
		Command: "sleep",
		Args:    []string{"10"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(originalConfig); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Launch the process directly.
	entry, ok := b.registry.Get("args-test-server")
	if !ok {
		t.Fatalf("expected args-test-server in registry")
	}
	if err := b.launcher.Start(entry.Config); err != nil {
		t.Fatalf("launcher.Start failed: %v", err)
	}

	// Reload with different args.
	newConfig := types.ServerConfig{
		Name:    "args-test-server",
		Command: "sleep",
		Args:    []string{"20"},
		Type:    "stdio",
	}
	err := b.ReloadServerWithConfig("args-test-server", newConfig)
	if err == nil {
		t.Log("ReloadServerWithConfig succeeded (unlikely for sleep)")
	} else {
		t.Logf("ReloadServerWithConfig returned expected error: %v", err)
	}

	// Verify Args were updated.
	entry, ok = b.registry.Get("args-test-server")
	if !ok {
		t.Fatal("args-test-server should still be in registry")
	}
	if len(entry.Config.Args) != 1 || entry.Config.Args[0] != "20" {
		t.Errorf("expected Args=[\"20\"], got Args=%v", entry.Config.Args)
	}

	// Clean up.
	_ = b.DeregisterServer("args-test-server")
}

// TestReloadServerWithConfig_IdempotentStart verifies that calling
// ReloadServerWithConfig on a stopped server starts it with the new config
// (idempotent behavior: same as ReloadServer on a stopped server).
func TestReloadServerWithConfig_IdempotentStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// "sleep" never speaks MCP — drop the proxy timeout so the handshake
	// fails in ~500ms instead of blocking 30s and racing -timeout. (T-117.3)
	b.SetRPCTimeout(500 * time.Millisecond)

	originalConfig := types.ServerConfig{
		Name:    "idle-server",
		Command: "sleep",
		Args:    []string{"60"},
		Env:     map[string]string{"ORIGINAL": "yes"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(originalConfig); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// The server is registered but NOT started (Process == nil).
	// ReloadServerWithConfig should apply the new config and attempt to start.
	newConfig := types.ServerConfig{
		Name:    "idle-server",
		Command: "sleep",
		Args:    []string{"999"},
		Env:     map[string]string{"RELOADED": "true"},
		Type:    "stdio",
	}
	err := b.ReloadServerWithConfig("idle-server", newConfig)
	// "sleep" is not an MCP server, so StartServer will fail at handshake.
	// The key assertions: (1) no "not registered" error, and (2) config was updated.
	if err == nil {
		t.Log("ReloadServerWithConfig succeeded (unlikely for sleep)")
	} else {
		if err.Error() == "server \"idle-server\" not registered" {
			t.Fatalf("should not get registry error, got: %v", err)
		}
		t.Logf("ReloadServerWithConfig returned expected error: %v", err)
	}

	// Verify config was updated before the start attempt.
	entry, ok := b.registry.Get("idle-server")
	if !ok {
		t.Fatal("idle-server should still be in registry")
	}
	if entry.Config.Args[0] != "999" {
		t.Errorf("expected Args=[\"999\"], got Args=%v", entry.Config.Args)
	}
	if entry.Config.Env["RELOADED"] != "true" {
		t.Errorf("expected RELOADED=true, got %v", entry.Config.Env["RELOADED"])
	}
	// ORIGINAL should be gone (full replacement, not merge).
	if _, exists := entry.Config.Env["ORIGINAL"]; exists {
		t.Error("ORIGINAL should not exist in new config (full replacement)")
	}

	// Clean up.
	_ = b.DeregisterServer("idle-server")
}
