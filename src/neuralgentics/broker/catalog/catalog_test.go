package catalog

import (
	"os"
	"testing"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/registry"
	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// helperRegistry creates a registry with the given configs and optional tool lists.
func helperRegistry(t *testing.T, configs []types.ServerConfig, toolsPerServer map[string][]types.ToolSummary) *registry.Registry {
	t.Helper()
	reg := registry.NewRegistry()
	for _, cfg := range configs {
		if err := reg.Register(cfg); err != nil {
			t.Fatalf("failed to register server %q: %v", cfg.Name, err)
		}
		if tools, ok := toolsPerServer[cfg.Name]; ok {
			entry, _ := reg.Get(cfg.Name)
			entry.Tools = tools
			reg.UpdateEntry(cfg.Name, entry)
		}
	}
	return reg
}

func TestBuild_EmptyRegistry(t *testing.T) {
	reg := registry.NewRegistry()
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cat.Servers))
	}
	if cat.TotalTools != 0 {
		t.Errorf("expected 0 total tools, got %d", cat.TotalTools)
	}
}

func TestBuild_TwoServers(t *testing.T) {
	configs := []types.ServerConfig{
		{
			Name:         "fs",
			Command:      "/usr/bin/fs-server",
			Description:  "File system operations",
			Capabilities: []string{"file", "io"},
		},
		{
			Name:         "web",
			Command:      "/usr/bin/web-server",
			Description:  "Web fetching tools",
			Capabilities: []string{"http", "browser"},
		},
	}
	toolsPerServer := map[string][]types.ToolSummary{
		"fs": {
			{Server: "fs", Name: "read_file", Description: "Read a file"},
			{Server: "fs", Name: "write_file", Description: "Write a file"},
		},
		"web": {
			{Server: "web", Name: "fetch_url", Description: "Fetch a URL"},
		},
	}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cat.Servers))
	}
	if cat.TotalTools != 3 {
		t.Errorf("expected total_tools=3, got %d", cat.TotalTools)
	}

	// Find servers by name for assertions.
	serverMap := make(map[string]ServerSummary)
	for _, s := range cat.Servers {
		serverMap[s.Name] = s
	}

	fsSummary, ok := serverMap["fs"]
	if !ok {
		t.Fatal("fs server not found in catalog")
	}
	if fsSummary.ToolsCount != 2 {
		t.Errorf("fs: expected tools_count=2, got %d", fsSummary.ToolsCount)
	}
	if fsSummary.Status != "stopped" {
		t.Errorf("fs: expected status=stopped, got %q", fsSummary.Status)
	}
	if fsSummary.Description != "File system operations" {
		t.Errorf("fs: expected description='File system operations', got %q", fsSummary.Description)
	}

	webSummary, ok := serverMap["web"]
	if !ok {
		t.Fatal("web server not found in catalog")
	}
	if webSummary.ToolsCount != 1 {
		t.Errorf("web: expected tools_count=1, got %d", webSummary.ToolsCount)
	}
}

func TestBuild_EmptyRole_IncludesAll(t *testing.T) {
	configs := []types.ServerConfig{
		{Name: "github-mcp", Command: "/usr/bin/gh", Description: "GitHub"},
		{Name: "playwright", Command: "/usr/bin/pw", Description: "Browser"},
	}
	toolsPerServer := map[string][]types.ToolSummary{}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 2 {
		t.Errorf("expected 2 servers with empty role, got %d", len(cat.Servers))
	}
}

func TestBuild_RoleFiltering(t *testing.T) {
	// Per T-040 DefaultServerRoles: github-mcp is restricted to boomerang-git
	// (plus orchestrator wildcard), and playwright is restricted to
	// tester/scraper/researcher roles. "coder" is denied both.
	configs := []types.ServerConfig{
		{Name: "github-mcp", Command: "/usr/bin/gh", Description: "GitHub"},
		{Name: "playwright", Command: "/usr/bin/pw", Description: "Browser"},
	}

	reg := helperRegistry(t, configs, nil)
	b := NewBuilder(reg)

	// "coder" role can NOT access github-mcp (boomerang-git only) and
	// can NOT access playwright (tester/scraper/researcher only).
	cat := b.Build("coder")
	names := make(map[string]bool)
	for _, s := range cat.Servers {
		names[s.Name] = true
	}
	if names["github-mcp"] {
		t.Error("expected coder to NOT have access to github-mcp (boomerang-git only per T-040)")
	}
	if names["playwright"] {
		t.Error("expected coder to NOT have access to playwright (tester/scraper/researcher only per T-040)")
	}
}

func TestBuild_DescriptionFallback(t *testing.T) {
	configs := []types.ServerConfig{
		{
			Name:    "nodesc",
			Command: "/usr/bin/nodesc",
			// No Description set.
		},
	}
	toolsPerServer := map[string][]types.ToolSummary{
		"nodesc": {
			{Server: "nodesc", Name: "do_something", Description: "This is a very long description that exceeds the eighty character limit for the fallback"},
		},
	}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}

	desc := cat.Servers[0].Description
	if desc == "" {
		t.Fatal("expected description fallback, got empty string")
	}
	// The fallback should be truncated to 80 chars max (including "...")
	if len(desc) > 80 {
		t.Errorf("description should be <= 80 chars, got %d: %q", len(desc), desc)
	}
	// Should end with "..."
	if len(cat.Servers[0].Description) > 0 && len(toolsPerServer["nodesc"][0].Description) > 80 {
		if desc[len(desc)-3:] != "..." {
			t.Errorf("truncated description should end with '...', got %q", desc)
		}
	}
}

func TestBuild_DescriptionFallback_ShortDesc(t *testing.T) {
	configs := []types.ServerConfig{
		{
			Name:    "shortdesc",
			Command: "/usr/bin/shortdesc",
		},
	}
	toolsPerServer := map[string][]types.ToolSummary{
		"shortdesc": {
			{Server: "shortdesc", Name: "ping", Description: "Ping a host"},
		},
	}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}
	if cat.Servers[0].Description != "Ping a host" {
		t.Errorf("expected 'Ping a host', got %q", cat.Servers[0].Description)
	}
}

func TestBuild_CapabilityInference(t *testing.T) {
	configs := []types.ServerConfig{
		{
			Name:    "browser-tools",
			Command: "/usr/bin/browser",
			// No Capabilities set — should be inferred from tool names.
		},
	}
	toolsPerServer := map[string][]types.ToolSummary{
		"browser-tools": {
			{Server: "browser-tools", Name: "browser_click", Description: "Click an element"},
			{Server: "browser-tools", Name: "browser_type", Description: "Type text"},
			{Server: "browser-tools", Name: "browser_navigate", Description: "Navigate to URL"},
			{Server: "browser-tools", Name: "screenshot", Description: "Take a screenshot"},
		},
	}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}

	caps := cat.Servers[0].Capabilities
	capMap := make(map[string]bool)
	for _, c := range caps {
		capMap[c] = true
	}

	// "browser" prefix should be inferred once (deduplicated).
	if !capMap["browser"] {
		t.Error("expected 'browser' capability to be inferred")
	}
	// "screenshot" (no underscore) should also be its own prefix.
	if !capMap["screenshot"] {
		t.Error("expected 'screenshot' capability to be inferred")
	}
	// "browser" should appear only once (deduplication).
	count := 0
	for _, c := range caps {
		if c == "browser" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected 'browser' to appear once due to dedup, got %d", count)
	}
}

func TestBuild_StatusRunning(t *testing.T) {
	configs := []types.ServerConfig{
		{Name: "running-srv", Command: "/usr/bin/srv"},
	}
	toolsPerServer := map[string][]types.ToolSummary{}

	reg := helperRegistry(t, configs, toolsPerServer)
	// Simulate a running process by setting Process field.
	entry, _ := reg.Get("running-srv")
	entry.Process = &os.Process{}
	reg.UpdateEntry("running-srv", entry)

	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}
	if cat.Servers[0].Status != "running" {
		t.Errorf("expected status=running, got %q", cat.Servers[0].Status)
	}
}

func TestBuild_StatusStopped(t *testing.T) {
	configs := []types.ServerConfig{
		{Name: "stopped-srv", Command: "/usr/bin/srv"},
	}
	toolsPerServer := map[string][]types.ToolSummary{}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}
	if cat.Servers[0].Status != "stopped" {
		t.Errorf("expected status=stopped, got %q", cat.Servers[0].Status)
	}
}

func TestBuild_StatusRegistered(t *testing.T) {
	configs := []types.ServerConfig{
		{Name: "noreg-srv"}, // No Command → registered status.
	}

	reg := helperRegistry(t, configs, nil)
	b := NewBuilder(reg)
	cat := b.Build("")

	if len(cat.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cat.Servers))
	}
	if cat.Servers[0].Status != "registered" {
		t.Errorf("expected status=registered, got %q", cat.Servers[0].Status)
	}
}

func TestBuild_TotalTools(t *testing.T) {
	configs := []types.ServerConfig{
		{Name: "a", Command: "/usr/bin/a", Description: "Alpha"},
		{Name: "b", Command: "/usr/bin/b", Description: "Beta"},
	}
	toolsPerServer := map[string][]types.ToolSummary{
		"a": {
			{Server: "a", Name: "tool1", Description: "Tool one"},
			{Server: "a", Name: "tool2", Description: "Tool two"},
		},
		"b": {
			{Server: "b", Name: "tool3", Description: "Tool three"},
			{Server: "b", Name: "tool4", Description: "Tool four"},
			{Server: "b", Name: "tool5", Description: "Tool five"},
		},
	}

	reg := helperRegistry(t, configs, toolsPerServer)
	b := NewBuilder(reg)
	cat := b.Build("")

	if cat.TotalTools != 5 {
		t.Errorf("expected total_tools=5, got %d", cat.TotalTools)
	}
}

func TestInferCapabilities_Dedup(t *testing.T) {
	tools := []types.ToolSummary{
		{Name: "browser_click"},
		{Name: "browser_type"},
		{Name: "browser_navigate"},
		{Name: "http_get"},
	}
	caps := inferCapabilities(tools)

	if len(caps) != 2 {
		t.Fatalf("expected 2 unique capabilities, got %d: %v", len(caps), caps)
	}
	capSet := make(map[string]bool)
	for _, c := range caps {
		capSet[c] = true
	}
	if !capSet["browser"] {
		t.Error("expected 'browser' in capabilities")
	}
	if !capSet["http"] {
		t.Error("expected 'http' in capabilities")
	}
}

func TestInferCapabilities_SingleToken(t *testing.T) {
	tools := []types.ToolSummary{
		{Name: "ping"}, // No underscore → whole name is the prefix.
	}
	caps := inferCapabilities(tools)
	if len(caps) != 1 || caps[0] != "ping" {
		t.Errorf("expected [ping], got %v", caps)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly 80 characters long including spaces and punctuation here!!", 80, "exactly 80 characters long including spaces and punctuation here!!"},
		{"a very long description that should be truncated to fit within the limit", 30, "a very long description ..."},
		{"ab", 2, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"}, // maxLen <= 3: just truncate
		{"abcdef", 5, "ab..."},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.maxLen)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
		}
	}
}
