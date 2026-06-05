package access

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CanAccess — basic logic tests
// ---------------------------------------------------------------------------

func TestCanAccess_DefaultAllow(t *testing.T) {
	ac := DefaultAccessControl()
	// "unknown-server" is not in DefaultServerRoles, so default allow.
	if !ac.CanAccess("coder", "unknown-server") {
		t.Error("expected default allow for unregistered server, got denied")
	}
}

func TestCanAccess_EmptyRolesList(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{
		"open-server": {},
	})
	if !ac.CanAccess("anyone", "open-server") {
		t.Error("expected empty roles list to allow all, got denied")
	}
}

func TestCanAccess_OrchestratorAccessAll(t *testing.T) {
	ac := DefaultAccessControl()
	for _, server := range ac.ServerNames() {
		if !ac.CanAccess("orchestrator", server) {
			t.Errorf("expected orchestrator to access %s, got denied", server)
		}
	}
	// Orchestrator also accesses unregistered servers.
	if !ac.CanAccess("orchestrator", "unknown-future-server") {
		t.Error("expected orchestrator to access unregistered server")
	}
}

func TestCanAccess_UnknownRole(t *testing.T) {
	ac := DefaultAccessControl()
	// Unknown role cannot access restricted servers.
	if ac.CanAccess("unknown-role", "playwright") {
		t.Error("expected unknown role to be denied access to playwright, got allowed")
	}
	// Unknown role CAN access baseline servers (memoryManager, neuralgentics) and unregistered.
	if !ac.CanAccess("unknown-role", "memoryManager") {
		t.Error("expected unknown role to access memoryManager (allow-all)")
	}
	if !ac.CanAccess("unknown-role", "neuralgentics") {
		t.Error("expected unknown role to access neuralgentics (allow-all)")
	}
	if !ac.CanAccess("unknown-role", "unknown-server") {
		t.Error("expected unknown role to access unregistered server (default allow)")
	}
}

func TestCanAccess_DefaultAllowForUnregisteredServer(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{
		"restricted": {RoleOrchestrator},
	})
	// "other-server" is not in the map → default allow.
	if !ac.CanAccess("anyone", "other-server") {
		t.Error("expected default allow for unregistered server")
	}
}

func TestCanAccess_EmptyRolesListAllowsAll(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{
		"wide-open": {},
	})
	// Empty role list → allow all.
	roles := []string{"coder", "explorer", "random-agent"}
	for _, role := range roles {
		if !ac.CanAccess(role, "wide-open") {
			t.Errorf("expected %q to access wide-open, got denied", role)
		}
	}
}

// ---------------------------------------------------------------------------
// CanAccess — full role × server matrix (all 22 roles × 8 servers)
// ---------------------------------------------------------------------------

func TestCanAccess_FullRoleServerMatrix(t *testing.T) {
	ac := DefaultAccessControl()

	// Define expected access: role → set of servers it CAN access (from DefaultServerRoles).
	// Baseline servers (memoryManager, neuralgentics) have empty lists → allow-all.
	// Unregistered servers → default allow.
	// Orchestrator → wildcard.
	type expected struct {
		server string
		allow  bool
	}

	// For each role, define expectations for ALL 8 registered servers.
	matrix := map[string][]expected{
		// Orchestrator: wildcard — all servers allowed.
		"orchestrator": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", true},
			{"playwright", true}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", true},
		},
		// coder: searxng, webfetch, websearch + baseline.
		"coder": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// architect: searxng, webfetch, websearch, markitdown + baseline.
		"architect": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", true},
		},
		// explorer: baseline only.
		"explorer": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// tester: playwright, webfetch, websearch + baseline.
		"tester": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", true}, {"searxng", false}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// writer: markitdown + baseline.
		"writer": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", true},
		},
		// linter: searxng, webfetch, websearch + baseline.
		"linter": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// git: searxng, webfetch, websearch, markitdown + baseline. (NOT github-mcp; only boomerang-git gets that.)
		"git": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", true},
		},
		// researcher: playwright, searxng, webfetch, websearch + baseline.
		"researcher": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", true}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// reviewer: baseline only.
		"reviewer": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// boomerang-coder: searxng, webfetch, websearch + baseline.
		"boomerang-coder": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// boomerang-architect: searxng, webfetch, websearch, markitdown + baseline.
		"boomerang-architect": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", true},
		},
		// boomerang-explorer: baseline only.
		"boomerang-explorer": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// boomerang-tester: playwright, webfetch, websearch + baseline.
		"boomerang-tester": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", true}, {"searxng", false}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// boomerang-linter: searxng, webfetch, websearch + baseline.
		"boomerang-linter": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// boomerang-git: github-mcp, searxng, webfetch, websearch, markitdown + baseline.
		"boomerang-git": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", true},
			{"playwright", false}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", true},
		},
		// boomerang-writer: markitdown + baseline.
		"boomerang-writer": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", true},
		},
		// boomerang-scraper: searxng, webfetch, websearch, playwright + baseline.
		"boomerang-scraper": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", true}, {"searxng", true}, {"webfetch", true},
			{"websearch", true}, {"markitdown", false},
		},
		// boomerang-release: markitdown + baseline (NO github-mcp per AGENTS.md).
		"boomerang-release": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", true},
		},
		// boomerang-init: baseline only.
		"boomerang-init": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// boomerang-handoff: baseline only.
		"boomerang-handoff": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// boomerang-agent-builder: baseline only.
		"boomerang-agent-builder": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
		// mcp-specialist: baseline only.
		"mcp-specialist": {
			{"memoryManager", true}, {"neuralgentics", true}, {"github-mcp", false},
			{"playwright", false}, {"searxng", false}, {"webfetch", false},
			{"websearch", false}, {"markitdown", false},
		},
	}

	for role, expectations := range matrix {
		for _, exp := range expectations {
			result := ac.CanAccess(role, exp.server)
			if result != exp.allow {
				t.Errorf("CanAccess(%q, %q) = %v, want %v", role, exp.server, result, exp.allow)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Specific role tests (subset of the matrix for clarity in test failures)
// ---------------------------------------------------------------------------

func TestCanAccess_BoomerangGit_GithubMCP(t *testing.T) {
	ac := DefaultAccessControl()
	if !ac.CanAccess("boomerang-git", "github-mcp") {
		t.Error("expected boomerang-git to access github-mcp")
	}
}

func TestCanAccess_Coder_DeniedGithubMCP(t *testing.T) {
	ac := DefaultAccessControl()
	if ac.CanAccess("coder", "github-mcp") {
		t.Error("expected coder to be denied access to github-mcp")
	}
}

func TestCanAccess_BoomerangRelease_DeniedGithubMCP(t *testing.T) {
	ac := DefaultAccessControl()
	// boomerang-release does NOT get github-mcp per AGENTS.md.
	if ac.CanAccess("boomerang-release", "github-mcp") {
		t.Error("expected boomerang-release to be denied access to github-mcp")
	}
}

func TestCanAccess_BoomerangScraper_Playwright(t *testing.T) {
	ac := DefaultAccessControl()
	if !ac.CanAccess("boomerang-scraper", "playwright") {
		t.Error("expected boomerang-scraper to access playwright")
	}
}

func TestCanAccess_Researcher_Playwright(t *testing.T) {
	ac := DefaultAccessControl()
	if !ac.CanAccess("researcher", "playwright") {
		t.Error("expected researcher to access playwright")
	}
}

func TestCanAccess_Git_DeniedGithubMCP(t *testing.T) {
	ac := DefaultAccessControl()
	// "git" (base role) does not get github-mcp; only "boomerang-git" does.
	if ac.CanAccess("git", "github-mcp") {
		t.Error("expected git (base role) to be denied access to github-mcp")
	}
}

// ---------------------------------------------------------------------------
// GetAccessibleServers
// ---------------------------------------------------------------------------

func TestGetAccessibleServers_Orchestrator(t *testing.T) {
	ac := DefaultAccessControl()
	servers := ac.GetAccessibleServers("orchestrator")

	expected := []string{"github-mcp", "markitdown", "memoryManager", "neuralgentics", "playwright", "searxng", "webfetch", "websearch"}
	if len(servers) != len(expected) {
		t.Fatalf("expected %d servers for orchestrator, got %d: %v", len(expected), len(servers), servers)
	}
	for i, exp := range expected {
		if servers[i] != exp {
			t.Errorf("expected server %q at index %d, got %q", exp, i, servers[i])
		}
	}
}

func TestGetAccessibleServers_BoomerangGit(t *testing.T) {
	ac := DefaultAccessControl()
	servers := ac.GetAccessibleServers("boomerang-git")

	// boomerang-git can access: github-mcp + searxng, webfetch, websearch, markitdown + baseline (memoryManager, neuralgentics)
	expected := []string{"github-mcp", "markitdown", "memoryManager", "neuralgentics", "searxng", "webfetch", "websearch"}
	if len(servers) != len(expected) {
		t.Fatalf("expected %d servers for boomerang-git, got %d: %v", len(expected), len(servers), servers)
	}
	for i, exp := range expected {
		if servers[i] != exp {
			t.Errorf("expected server %q at index %d, got %q", exp, i, servers[i])
		}
	}
}

func TestGetAccessibleServers_Explorer(t *testing.T) {
	ac := DefaultAccessControl()
	servers := ac.GetAccessibleServers("explorer")
	// explorer can only access baseline servers (allow-all).
	expected := []string{"memoryManager", "neuralgentics"}
	if len(servers) != len(expected) {
		t.Fatalf("expected %d servers for explorer, got %d: %v", len(expected), len(servers), servers)
	}
	for i, exp := range expected {
		if servers[i] != exp {
			t.Errorf("expected server %q at index %d, got %q", exp, i, servers[i])
		}
	}
}

func TestGetAccessibleServers_SortedOrder(t *testing.T) {
	ac := DefaultAccessControl()
	for _, role := range []string{"orchestrator", "boomerang-coder", "researcher", "explorer"} {
		servers := ac.GetAccessibleServers(role)
		for i := 1; i < len(servers); i++ {
			if servers[i] < servers[i-1] {
				t.Errorf("servers not sorted for role %q: %q before %q", role, servers[i-1], servers[i])
			}
		}
	}
}

func TestGetAccessibleServers_NoServers(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{})
	servers := ac.GetAccessibleServers("coder")
	if len(servers) != 0 {
		t.Errorf("expected no servers for empty AccessControl, got %v", servers)
	}
}

// ---------------------------------------------------------------------------
// Grant / Revoke
// ---------------------------------------------------------------------------

func TestGrant_AddRoleToRestrictedServer(t *testing.T) {
	ac := DefaultAccessControl()
	// coder does NOT have github-mcp access by default.
	if ac.CanAccess("coder", "github-mcp") {
		t.Fatal("setup: coder should NOT have github-mcp access before grant")
	}

	// Grant coder access to github-mcp.
	ac.Grant("coder", "github-mcp")

	if !ac.CanAccess("coder", "github-mcp") {
		t.Error("expected coder to have github-mcp access after grant")
	}
}

func TestGrant_Idempotent(t *testing.T) {
	ac := DefaultAccessControl()
	ac.Grant("researcher", "searxng") // researcher already has searxng.
	ac.Grant("researcher", "searxng") // Grant again — should be no-op.

	// Verify researcher can still access searxng.
	if !ac.CanAccess("researcher", "searxng") {
		t.Error("expected researcher to still access searxng after duplicate grant")
	}
}

func TestGrant_NewServer(t *testing.T) {
	ac := DefaultAccessControl()
	// "custom-server" not in DefaultServerRoles → default allow for all.
	if !ac.CanAccess("explorer", "custom-server") {
		t.Fatal("setup: unregistered server should be default-allow")
	}

	// Granting explorer to custom-server restricts it.
	ac.Grant("explorer", "custom-server")

	// explorer can still access.
	if !ac.CanAccess("explorer", "custom-server") {
		t.Error("expected explorer to access custom-server after grant")
	}

	// But coder cannot (it was default-allow, now restricted).
	if ac.CanAccess("coder", "custom-server") {
		t.Error("expected coder to be denied custom-server after grant restricted it")
	}
}

func TestRevoke_RemoveRole(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{
		"test-server": {RoleCoder, RoleArchitect},
	})

	// Architect has access.
	if !ac.CanAccess("architect", "test-server") {
		t.Fatal("setup: architect should have test-server access")
	}

	// Revoke architect.
	ac.Revoke("architect", "test-server")

	if ac.CanAccess("architect", "test-server") {
		t.Error("expected architect to be revoked from test-server")
	}

	// Coder still has access.
	if !ac.CanAccess("coder", "test-server") {
		t.Error("expected coder to still have test-server access after architect revocation")
	}
}

func TestRevoke_UnregisteredServer(t *testing.T) {
	ac := DefaultAccessControl()
	// Revoking from an unregistered server should be a no-op.
	ac.Revoke("coder", "nonexistent-server")
	// Unregistered server still default-allow.
	if !ac.CanAccess("coder", "nonexistent-server") {
		t.Error("expected default allow after revoke on unregistered server")
	}
}

func TestRevoke_AllRoles(t *testing.T) {
	ac := NewAccessControl(map[string][]Role{
		"strict-server": {RoleCoder},
	})

	ac.Revoke("coder", "strict-server")
	// After removing all roles from the list, the list becomes empty but
	// still exists in the map → empty list means allow-all per CanAccess logic.
	// Wait — actually empty list IS allow-all. Let me verify.
	roles := ac.serverRoles["strict-server"]
	if len(roles) != 0 {
		t.Errorf("expected empty roles list after revoking all, got %d roles", len(roles))
	}
	// Empty list → allow-all.
	if !ac.CanAccess("anyone", "strict-server") {
		t.Error("expected allow-all after all roles revoked (empty list)")
	}
}

// ---------------------------------------------------------------------------
// ServerNames / AllRoles
// ---------------------------------------------------------------------------

func TestServerNames(t *testing.T) {
	ac := DefaultAccessControl()
	names := ac.ServerNames()

	if len(names) != len(DefaultServerRoles) {
		t.Errorf("expected %d server names, got %d", len(DefaultServerRoles), len(names))
	}

	// Verify sorted.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("server names not sorted: %q before %q", names[i-1], names[i])
		}
	}

	// Verify expected servers present.
	expectedServers := []string{"memoryManager", "neuralgentics", "github-mcp", "playwright", "searxng", "webfetch", "websearch", "markitdown"}
	for _, exp := range expectedServers {
		found := false
		for _, n := range names {
			if n == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected server %q in ServerNames(), not found", exp)
		}
	}
}

func TestAllRoles(t *testing.T) {
	roles := AllRoles()
	expectedCount := 23 // 23 Role constants defined.
	if len(roles) != expectedCount {
		t.Errorf("expected %d roles from AllRoles(), got %d", expectedCount, len(roles))
	}

	// Verify some specific roles exist.
	knownRoles := map[string]bool{
		"orchestrator": true, "coder": true, "boomerang-git": true,
		"mcp-specialist": true, "reviewer": true, "boomerang-agent-builder": true,
	}
	for _, r := range roles {
		if knownRoles[string(r)] {
			delete(knownRoles, string(r))
		}
	}
	if len(knownRoles) > 0 {
		t.Errorf("missing expected roles: %v", knownRoles)
	}
}

// ---------------------------------------------------------------------------
// ErrUnauthorized
// ---------------------------------------------------------------------------

func TestErrUnauthorized_Error(t *testing.T) {
	err := ErrUnauthorized{
		Role:   "coder",
		Server: "playwright",
		Reason: "role coder cannot access server playwright",
	}
	msg := err.Error()
	if !strings.Contains(msg, "unauthorized:") {
		t.Errorf("expected error to contain 'unauthorized:', got %q", msg)
	}
	if !strings.Contains(msg, err.Reason) {
		t.Errorf("expected error to contain reason %q, got %q", err.Reason, msg)
	}
}

func TestErrUnauthorized_ErrorNoReason(t *testing.T) {
	err := ErrUnauthorized{
		Role:   "explorer",
		Server: "github-mcp",
	}
	msg := err.Error()
	if !strings.Contains(msg, "explorer") || !strings.Contains(msg, "github-mcp") {
		t.Errorf("expected error to contain role and server, got %q", msg)
	}
}

func TestErrUnauthorized_ToJSONError(t *testing.T) {
	err := ErrUnauthorized{
		Role:             "coder",
		Server:           "playwright",
		Reason:           "role coder cannot access server playwright",
		AvailableServers: []string{"github-mcp", "memoryManager"},
	}
	jsonErr := err.ToJSONError()

	// Verify top-level "error" key.
	errObj, ok := jsonErr["error"]
	if !ok {
		t.Fatal("expected 'error' key in ToJSONError output")
	}

	errMap, ok := errObj.(map[string]any)
	if !ok {
		t.Fatalf("expected error to be map[string]any, got %T", errObj)
	}

	// Verify code.
	code, ok := errMap["code"]
	if !ok {
		t.Fatal("expected 'code' key in error object")
	}
	if code != -32001 {
		t.Errorf("expected code -32001, got %v", code)
	}

	// Verify message.
	msg, ok := errMap["message"]
	if !ok {
		t.Fatal("expected 'message' key in error object")
	}
	msgStr, ok := msg.(string)
	if !ok {
		t.Fatalf("expected message to be string, got %T", msg)
	}
	if !strings.Contains(msgStr, "coder") || !strings.Contains(msgStr, "playwright") {
		t.Errorf("expected message to contain role and server, got %q", msgStr)
	}

	// Verify data.
	data, ok := errMap["data"]
	if !ok {
		t.Fatal("expected 'data' key in error object")
	}
	dataMap, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map[string]any, got %T", data)
	}

	// Verify available_servers.
	avail, ok := dataMap["available_servers"]
	if !ok {
		t.Fatal("expected 'available_servers' key in data")
	}
	availSlice, ok := avail.([]string)
	if !ok {
		t.Fatalf("expected available_servers to be []string, got %T", avail)
	}
	if len(availSlice) != 2 {
		t.Errorf("expected 2 available servers, got %d", len(availSlice))
	}

	// Verify suggestion.
	suggestion, ok := dataMap["suggestion"]
	if !ok {
		t.Fatal("expected 'suggestion' key in data")
	}
	sugStr, ok := suggestion.(string)
	if !ok {
		t.Fatalf("expected suggestion to be string, got %T", suggestion)
	}
	if !strings.Contains(sugStr, "coder can access:") {
		t.Errorf("expected suggestion to contain 'coder can access:', got %q", sugStr)
	}
	if !strings.Contains(sugStr, "MatchIntent()") {
		t.Errorf("expected suggestion to contain 'MatchIntent()', got %q", sugStr)
	}
}

// ---------------------------------------------------------------------------
// Defensive copy / immutability
// ---------------------------------------------------------------------------

func TestNewAccessControl_DefensiveCopy(t *testing.T) {
	original := map[string][]Role{
		"test-server": {RoleCoder},
	}
	ac := NewAccessControl(original)

	// Mutating original should not affect AccessControl.
	original["test-server"] = []Role{RoleArchitect}

	if !ac.CanAccess("coder", "test-server") {
		t.Error("expected coder to still access test-server after external mutation")
	}
}

// ---------------------------------------------------------------------------
// Broker.Call integration — access control enforcement
// ---------------------------------------------------------------------------

func TestCanAccess_ResearcherRole(t *testing.T) {
	ac := DefaultAccessControl()
	// Researcher can access: playwright, searxng
	if !ac.CanAccess("researcher", "playwright") {
		t.Error("expected researcher to access playwright")
	}
	if !ac.CanAccess("researcher", "searxng") {
		t.Error("expected researcher to access searxng")
	}
	// Researcher cannot access: github-mcp
	if ac.CanAccess("researcher", "github-mcp") {
		t.Error("expected researcher to be denied github-mcp")
	}
}

func TestCanAccess_BoomerangCoder(t *testing.T) {
	ac := DefaultAccessControl()
	// boomerang-coder has: searxng, webfetch, websearch + baseline.
	if !ac.CanAccess("boomerang-coder", "searxng") {
		t.Error("expected boomerang-coder to access searxng")
	}
	if !ac.CanAccess("boomerang-coder", "webfetch") {
		t.Error("expected boomerang-coder to access webfetch")
	}
	if !ac.CanAccess("boomerang-coder", "websearch") {
		t.Error("expected boomerang-coder to access websearch")
	}
	// boomerang-coder does NOT have: github-mcp, playwright, markitdown.
	if ac.CanAccess("boomerang-coder", "github-mcp") {
		t.Error("expected boomerang-coder to be denied github-mcp")
	}
	if ac.CanAccess("boomerang-coder", "playwright") {
		t.Error("expected boomerang-coder to be denied playwright")
	}
	if ac.CanAccess("boomerang-coder", "markitdown") {
		t.Error("expected boomerang-coder to be denied markitdown")
	}
}

func TestCanAccess_MCPSpecialist(t *testing.T) {
	ac := DefaultAccessControl()
	// mcp-specialist can access: baseline (memoryManager, neuralgentics) only.
	if !ac.CanAccess("mcp-specialist", "memoryManager") {
		t.Error("expected mcp-specialist to access memoryManager")
	}
	if !ac.CanAccess("mcp-specialist", "neuralgentics") {
		t.Error("expected mcp-specialist to access neuralgentics")
	}
	// mcp-specialist cannot access restricted servers.
	if ac.CanAccess("mcp-specialist", "github-mcp") {
		t.Error("expected mcp-specialist to be denied github-mcp")
	}
	if ac.CanAccess("mcp-specialist", "playwright") {
		t.Error("expected mcp-specialist to be denied playwright")
	}
}
