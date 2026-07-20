package broker

import (
	"context"
	"strings"
	"testing"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/access"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// ---------------------------------------------------------------------------
// DeregisterServer tests
// ---------------------------------------------------------------------------

// TestDeregisterServer_Running verifies that DeregisterServer stops
// a running server process and removes it from the registry.
func TestDeregisterServer_Running(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()

	config := types.ServerConfig{
		Name:    "der-running",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Launch the process via the launcher (bypass MCP handshake since sleep isn't MCP).
	entry, ok := b.registry.Get("der-running")
	if !ok {
		t.Fatalf("expected der-running in registry")
	}
	if err := b.launcher.Start(entry.Config); err != nil {
		t.Fatalf("launcher.Start failed: %v", err)
	}

	// Verify process is running.
	entry, _ = b.registry.Get("der-running")
	if entry.Process == nil {
		t.Fatal("expected process to be running after launcher.Start")
	}

	// Deregister should stop the process and remove the entry.
	if err := b.DeregisterServer("der-running"); err != nil {
		t.Fatalf("DeregisterServer failed: %v", err)
	}

	// Verify the server is gone from the registry.
	statuses := b.ListServers()
	for _, s := range statuses {
		if s.Name == "der-running" {
			t.Error("der-running should be deregistered")
		}
	}
}

// TestDeregisterServer_StoppedServer verifies that DeregisterServer
// removes a registered-but-not-running server from the registry.
func TestDeregisterServer_StoppedServer(t *testing.T) {
	b := NewBroker()

	config := types.ServerConfig{
		Name:    "der-stopped",
		Command: "echo",
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	if err := b.DeregisterServer("der-stopped"); err != nil {
		t.Fatalf("DeregisterServer failed: %v", err)
	}

	// Verify it's gone.
	statuses := b.ListServers()
	for _, s := range statuses {
		if s.Name == "der-stopped" {
			t.Error("der-stopped should be removed from registry")
		}
	}
}

// TestDeregisterServer_NotRegistered verifies that DeregisterServer returns
// an error when the server doesn't exist in the registry.
func TestDeregisterServer_NotRegistered(t *testing.T) {
	b := NewBroker()

	err := b.DeregisterServer("nonexistent")
	if err == nil {
		t.Fatal("expected error when deregistering nonexistent server")
	}
}

// TestDeregisterServer_DoesNotKillSharedProxy verifies that deregistering
// one server does NOT stop the shared proxy, which would kill the async
// reader for all other servers. This is a regression test for the bug where
// DeregisterServer unconditionally called b.proxy.Stop().
func TestDeregisterServer_DoesNotKillSharedProxy(t *testing.T) {
	b := NewBroker()

	// Register two servers.
	serverA := types.ServerConfig{
		Name:    "server-a",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	serverB := types.ServerConfig{
		Name:    "server-b",
		Command: "sleep",
		Args:    []string{"60"},
		Type:    "stdio",
	}
	if err := b.RegisterServer(serverA); err != nil {
		t.Fatalf("RegisterServer server-a failed: %v", err)
	}
	if err := b.RegisterServer(serverB); err != nil {
		t.Fatalf("RegisterServer server-b failed: %v", err)
	}

	// Start both servers via the launcher (bypass MCP handshake since
	// sleep isn't an MCP server). We need real processes with pipes.
	entryA, ok := b.registry.Get("server-a")
	if !ok {
		t.Fatal("expected server-a in registry")
	}
	if err := b.launcher.Start(entryA.Config); err != nil {
		t.Fatalf("launcher.Start server-a failed: %v", err)
	}
	entryB, ok := b.registry.Get("server-b")
	if !ok {
		t.Fatal("expected server-b in registry")
	}
	if err := b.launcher.Start(entryB.Config); err != nil {
		t.Fatalf("launcher.Start server-b failed: %v", err)
	}

	// Verify both processes are running.
	entryA, _ = b.registry.Get("server-a")
	entryB, _ = b.registry.Get("server-b")
	if entryA.Process == nil {
		t.Fatal("expected server-a process to be running")
	}
	if entryB.Process == nil {
		t.Fatal("expected server-b process to be running")
	}

	// Deregister server-a only. This MUST NOT stop the shared proxy.
	if err := b.DeregisterServer("server-a"); err != nil {
		t.Fatalf("DeregisterServer server-a failed: %v", err)
	}

	// Verify server-a is gone from the registry.
	statuses := b.ListServers()
	for _, s := range statuses {
		if s.Name == "server-a" {
			t.Error("server-a should be deregistered")
		}
	}

	// Verify server-b is still in the registry and still has its process.
	entryB, ok = b.registry.Get("server-b")
	if !ok {
		t.Fatal("expected server-b to still be registered")
	}
	if entryB.Process == nil {
		t.Error("expected server-b process to still be running after server-a was deregistered")
	}

	// Verify the proxy's running state is not disrupted.
	// Before the fix, b.proxy.Stop() was called, setting running=false.
	// After the fix, the proxy should remain usable.
	// We can't directly verify the proxy's internal state from here,
	// but we can verify that server-b can still be deregistered cleanly.
	if err := b.DeregisterServer("server-b"); err != nil {
		t.Fatalf("DeregisterServer server-b failed after server-a deregister: %v", err)
	}

	// Verify both servers are gone.
	statuses = b.ListServers()
	if len(statuses) != 0 {
		t.Errorf("expected 0 servers after deregistering both, got %d", len(statuses))
	}
}

// ---------------------------------------------------------------------------
// ListServers tests
// ---------------------------------------------------------------------------

// TestListServers_Empty verifies that ListServers returns an empty slice
// when no servers are registered.
func TestListServers_Empty(t *testing.T) {
	b := NewBroker()

	statuses := b.ListServers()
	if len(statuses) != 0 {
		t.Errorf("expected 0 servers, got %d", len(statuses))
	}
}

// TestListServers_Multiple verifies that ListServers returns all registered
// servers with correct status information.
func TestListServers_Multiple(t *testing.T) {
	b := NewBroker()

	servers := []types.ServerConfig{
		{Name: "srv-a", Command: "echo", Type: "stdio"},
		{Name: "srv-b", Command: "cat", Type: "stdio"},
		{Name: "srv-c", Command: "true", Type: "stdio"},
	}
	for _, s := range servers {
		if err := b.RegisterServer(s); err != nil {
			t.Fatalf("RegisterServer %q failed: %v", s.Name, err)
		}
	}

	statuses := b.ListServers()
	if len(statuses) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(statuses))
	}

	names := make(map[string]bool)
	for _, s := range statuses {
		names[s.Name] = true
		if s.Running {
			t.Errorf("expected %q to not be running, got running", s.Name)
		}
	}
	for _, cfg := range servers {
		if !names[cfg.Name] {
			t.Errorf("expected %q in server list", cfg.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// ListTools tests
// ---------------------------------------------------------------------------

// TestListTools_Empty verifies that ListTools returns an empty slice
// when no tools are registered.
func TestListTools_Empty(t *testing.T) {
	b := NewBroker()

	tools := b.ListTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for empty broker, got %d", len(tools))
	}
}

// TestListTools_WithTools verifies that ListTools returns tool summaries
// from all registered servers.
func TestListTools_WithTools(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name: "srv-a", Command: "echo", Type: "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}
	if err := b.RegisterServer(types.ServerConfig{
		Name: "srv-b", Command: "echo", Type: "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Set tools on srv-a.
	b.SetTools("srv-a", []types.ToolSummary{
		{Server: "srv-a", Name: "read_file", Description: "Read a file"},
		{Server: "srv-a", Name: "write_file", Description: "Write a file"},
	})

	// Set tools on srv-b.
	b.SetTools("srv-b", []types.ToolSummary{
		{Server: "srv-b", Name: "search", Description: "Search the web"},
	})

	tools := b.ListTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools total, got %d", len(tools))
	}

	// Verify all 3 tools are present.
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{"read_file", "write_file", "search"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q in ListTools result", name)
		}
	}
}

// ---------------------------------------------------------------------------
// BuildServerCatalog tests
// ---------------------------------------------------------------------------

// TestBuildServerCatalog_EmptyRole verifies that BuildServerCatalog with an
// empty role includes all registered servers.
func TestBuildServerCatalog_EmptyRole(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name:    "github-mcp",
		Command: "/usr/bin/gh",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}
	if err := b.RegisterServer(types.ServerConfig{
		Name:    "playwright",
		Command: "/usr/bin/pw",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	cat := b.BuildServerCatalog("")
	if len(cat.Servers) != 2 {
		t.Errorf("expected 2 servers with empty role, got %d", len(cat.Servers))
	}
}

// TestBuildServerCatalog_FilteredByRole verifies that BuildServerCatalog
// filters servers based on role access control.
func TestBuildServerCatalog_FilteredByRole(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name:    "github-mcp",
		Command: "/usr/bin/gh",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}
	if err := b.RegisterServer(types.ServerConfig{
		Name:    "playwright",
		Command: "/usr/bin/pw",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Per T-040 DefaultServerRoles: "coder" can NOT access github-mcp
	// (boomerang-git only) and can NOT access playwright
	// (tester/scraper/researcher only).
	cat := b.BuildServerCatalog("coder")
	names := make(map[string]bool)
	for _, s := range cat.Servers {
		names[s.Name] = true
	}
	if names["github-mcp"] {
		t.Error("expected coder to NOT have access to github-mcp (boomerang-git only per T-040)")
	}
	if names["playwright"] {
		t.Error("expected coder NOT to have access to playwright")
	}
}

// ---------------------------------------------------------------------------
// ExpandServer tests
// ---------------------------------------------------------------------------

// TestExpandServer_Found verifies that ExpandServer returns a valid
// ToolCatalog for a registered server.
func TestExpandServer_Found(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	b.SetTools("test-server", []types.ToolSummary{
		{Server: "test-server", Name: "tool_one", Description: "First tool"},
		{Server: "test-server", Name: "tool_two", Description: "Second tool"},
	})

	tc, err := b.ExpandServer("test-server")
	if err != nil {
		t.Fatalf("ExpandServer failed: %v", err)
	}
	if tc.Server != "test-server" {
		t.Errorf("expected Server=test-server, got %q", tc.Server)
	}
	if tc.Status != "stopped" {
		t.Errorf("expected Status=stopped, got %q", tc.Status)
	}
	if len(tc.Tools) != 2 {
		t.Fatalf("expected 2 tools in catalog, got %d", len(tc.Tools))
	}
	if tc.Tools[0].Name != "tool_one" {
		t.Errorf("expected tool_one, got %q", tc.Tools[0].Name)
	}
	if tc.Tools[1].Name != "tool_two" {
		t.Errorf("expected tool_two, got %q", tc.Tools[1].Name)
	}
}

// TestExpandServer_NotFound verifies that ExpandServer returns an error
// for a nonexistent server.
func TestExpandServer_NotFound(t *testing.T) {
	b := NewBroker()

	_, err := b.ExpandServer("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent server, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// InjectPrompt tests
// ---------------------------------------------------------------------------

// TestInjectPrompt_ReturnsNonEmpty verifies that InjectPrompt returns a
// non-empty string containing expected content.
func TestInjectPrompt_ReturnsNonEmpty(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name:         "fs",
		Command:      "/usr/bin/fs",
		Type:         "stdio",
		Description:  "File system operations",
		Capabilities: []string{"file", "io"},
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	b.SetTools("fs", []types.ToolSummary{
		{Server: "fs", Name: "read_file", Description: "Read a file"},
	})

	prompt, err := b.InjectPrompt("")
	if err != nil {
		t.Fatalf("InjectPrompt failed: %v", err)
	}
	if prompt == "" {
		t.Fatal("expected non-empty prompt output")
	}
	if !strings.Contains(prompt, "## Available MCP Servers") {
		t.Error("expected '## Available MCP Servers' header in prompt")
	}
	if !strings.Contains(prompt, "fs") {
		t.Error("expected 'fs' server name in prompt")
	}
	if !strings.Contains(prompt, "MatchIntent") {
		t.Error("expected 'MatchIntent' reference in prompt")
	}
}

// ---------------------------------------------------------------------------
// AccessControl tests (via Broker.AccessControl())
// ---------------------------------------------------------------------------

// TestAccessControl_DefaultRoles verifies that the default broker includes
// an AccessControl instance with DefaultServerRoles.
func TestAccessControl_DefaultRoles(t *testing.T) {
	b := NewBroker()

	ac := b.AccessControl()
	if ac == nil {
		t.Fatal("expected non-nil AccessControl from broker")
	}

	// Verify orchestrator has access to all default servers.
	for _, server := range []string{"github-mcp", "memoryManager", "playwright", "searxng", "markitdown"} {
		if !ac.CanAccess("orchestrator", server) {
			t.Errorf("expected orchestrator to access %s", server)
		}
	}
}

// TestAccessControl_UnauthorizedCall verifies that calling a restricted
// server with an unauthorized role returns ErrUnauthorized.
func TestAccessControl_UnauthorizedCall(t *testing.T) {
	b := NewBroker()

	// Register a server that the "coder" role cannot access (playwright).
	if err := b.RegisterServer(types.ServerConfig{
		Name:    "playwright",
		Command: "/usr/bin/pw",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Call with unauthorized role should return ErrUnauthorized.
	_, err := b.Call("coder", "playwright", "some_tool", nil)
	if err == nil {
		t.Fatal("expected error when calling unauthorized server, got nil")
	}

	// Verify it's an ErrUnauthorized.
	unauthErr, ok := err.(access.ErrUnauthorized)
	if !ok {
		t.Fatalf("expected access.ErrUnauthorized, got %T: %v", err, err)
	}
	if unauthErr.Role != "coder" {
		t.Errorf("expected Role=coder, got %q", unauthErr.Role)
	}
	if unauthErr.Server != "playwright" {
		t.Errorf("expected Server=playwright, got %q", unauthErr.Server)
	}
}

// ---------------------------------------------------------------------------
// SetTools tests
// ---------------------------------------------------------------------------

// TestSetTools_ExistingServer verifies that SetTools updates tools for
// an existing registered server.
func TestSetTools_ExistingServer(t *testing.T) {
	b := NewBroker()

	if err := b.RegisterServer(types.ServerConfig{
		Name:    "tool-srv",
		Command: "echo",
		Type:    "stdio",
	}); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Initially, the server should have no tools.
	tools := b.ListTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools initially, got %d", len(tools))
	}

	// Set tools.
	newTools := []types.ToolSummary{
		{Server: "tool-srv", Name: "add_memory", Description: "Add a memory"},
		{Server: "tool-srv", Name: "query_memory", Description: "Query memories"},
	}
	b.SetTools("tool-srv", newTools)

	// Verify tools are set.
	tools = b.ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools after SetTools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, t2 := range tools {
		names[t2.Name] = true
	}
	if !names["add_memory"] {
		t.Error("expected 'add_memory' in tools")
	}
	if !names["query_memory"] {
		t.Error("expected 'query_memory' in tools")
	}

	// Overwrite tools.
	updatedTools := []types.ToolSummary{
		{Server: "tool-srv", Name: "search_project", Description: "Search project"},
	}
	b.SetTools("tool-srv", updatedTools)

	tools = b.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool after overwrite, got %d", len(tools))
	}
	if tools[0].Name != "search_project" {
		t.Errorf("expected 'search_project', got %q", tools[0].Name)
	}
}

// TestSetTools_NonexistentServer verifies that SetTools is a no-op when
// the server name doesn't exist in the registry.
func TestSetTools_NonexistentServer(t *testing.T) {
	b := NewBroker()

	// SetTools on a nonexistent server should not panic.
	b.SetTools("ghost-server", []types.ToolSummary{
		{Server: "ghost-server", Name: "phantom", Description: "Does not exist"},
	})

	// Verify no tools exist.
	tools := b.ListTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for nonexistent server, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// Call timeout tests
// ---------------------------------------------------------------------------

// TestBroker_Call_HasTimeout verifies that Broker.Call does not hang
// indefinitely when the underlying server is unresponsive. The proxy's
// sendRPC has a 30-second hardcoded timeout, but Broker.Call itself does
// NOT accept a context.Context parameter.
//
// This is a CHARACTERIZATION test: it documents the current behavior
// (30s hardcoded proxy timeout, no caller-visible timeout) so that if a
// future refactor adds ctx context.Context to Call (per reviewer finding #7),
// we have a regression test to ensure the timeout still works.
//
// TODO: When Call gains a ctx parameter, update this test to use it.
func TestBroker_Call_HasTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	b := NewBroker()
	// Drop the proxy's 30s RPC timeout to a few hundred ms so the MCP
	// handshake against "sleep 999" fails fast. Without this, sendRPC
	// blocks for the full 30s and races the test binary's -timeout,
	// making the test appear to hang. See T-117.3.
	b.SetRPCTimeout(500 * time.Millisecond)

	// Register a server that will not respond (sleep + bogus command).
	// The launcher will start the process, but the MCP handshake will
	// never complete since sleep doesn't speak JSON-RPC on stdin.
	config := types.ServerConfig{
		Name:    "slow-server",
		Command: "sleep",
		Args:    []string{"999"}, // long enough that timeout fires, not process exit
		Type:    "stdio",
	}
	if err := b.RegisterServer(config); err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}

	// Start the server — this will fail at the Initialize step because
	// "sleep" doesn't speak MCP. But the process is started.
	_ = b.StartServer("slow-server")

	// Use an overall test context so we don't hang the test suite.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Channel to collect the Call result so we can enforce a timeout.
	type callResult struct {
		result map[string]any
		err    error
	}
	ch := make(chan callResult, 1)

	go func() {
		r, e := b.Call("orchestrator", "slow-server", "unknown_tool", nil)
		ch <- callResult{result: r, err: e}
	}()

	select {
	case res := <-ch:
		// Call returned — should be an error (either timeout from proxy's
		// 30s sendRPC, or access denied, or server-not-initialized).
		if res.err == nil {
			t.Error("expected error when calling an unresponsive server, got nil result")
		}
		t.Logf("Call returned error (expected): %v", res.err)
	case <-ctx.Done():
		t.Fatal("Call hung indefinitely — no timeout mechanism in place. " +
			"The proxy's sendRPC has a 30s timeout, but Call does not accept a context. " +
			"See reviewer finding #7: add ctx context.Context to Call.")
	}

	// Clean up.
	_ = b.DeregisterServer("slow-server")
}
