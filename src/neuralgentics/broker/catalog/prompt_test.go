package catalog

import (
	"strings"
	"testing"
)

func TestFormatForPrompt_EmptyCatalog(t *testing.T) {
	cat := ServerCatalog{}
	result := FormatForPrompt(cat)

	if !strings.Contains(result, "## Available MCP Servers") {
		t.Error("expected markdown header in empty catalog output")
	}
	if !strings.Contains(result, "No MCP servers") {
		t.Error("expected 'No MCP servers' message for empty catalog")
	}
}

func TestFormatForPrompt_TwoServers(t *testing.T) {
	cat := ServerCatalog{
		Servers: []ServerSummary{
			{
				Name:         "fs",
				Description:  "File system operations",
				Capabilities: []string{"file", "io"},
				ToolsCount:   3,
				Status:       "running",
			},
			{
				Name:         "web",
				Description:  "Web fetching tools",
				Capabilities: []string{"http", "browser"},
				ToolsCount:   2,
				Status:       "stopped",
			},
		},
		TotalTools: 5,
	}

	result := FormatForPrompt(cat)

	// Check header section.
	if !strings.Contains(result, "## Available MCP Servers") {
		t.Error("expected main header")
	}
	if !strings.Contains(result, "MatchIntent") {
		t.Error("expected MatchIntent reference in prompt")
	}

	// Check table header.
	if !strings.Contains(result, "| Server | Description |") {
		t.Error("expected markdown table header row")
	}
	if !strings.Contains(result, "|--------|-------------|") {
		t.Error("expected markdown table separator")
	}

	// Check server rows.
	if !strings.Contains(result, "| fs |") {
		t.Error("expected fs server row")
	}
	if !strings.Contains(result, "| web |") {
		t.Error("expected web server row")
	}
	if !strings.Contains(result, "File system operations") {
		t.Error("expected fs description in output")
	}
	if !strings.Contains(result, "Web fetching tools") {
		t.Error("expected web description in output")
	}

	// Check "How to Use" section.
	if !strings.Contains(result, "### How to Use MCP Tools") {
		t.Error("expected 'How to Use MCP Tools' section")
	}
	if !strings.Contains(result, "broker.ExpandServer") {
		t.Error("expected ExpandServer reference")
	}
}

func TestFormatForPrompt_RunningCount(t *testing.T) {
	cat := ServerCatalog{
		Servers: []ServerSummary{
			{Name: "a", Description: "Alpha", Capabilities: []string{"test"}, ToolsCount: 2, Status: "running"},
			{Name: "b", Description: "Beta", Capabilities: []string{"test"}, ToolsCount: 1, Status: "running"},
			{Name: "c", Description: "Gamma", Capabilities: []string{"test"}, ToolsCount: 3, Status: "stopped"},
		},
		TotalTools: 6,
	}

	result := FormatForPrompt(cat)

	if !strings.Contains(result, "2 running") {
		t.Errorf("expected '2 running' in header, output:\n%s", result)
	}
}

func TestFormatForPrompt_TotalTools(t *testing.T) {
	cat := ServerCatalog{
		Servers: []ServerSummary{
			{Name: "a", Description: "Alpha", Capabilities: []string{"test"}, ToolsCount: 4, Status: "running"},
			{Name: "b", Description: "Beta", Capabilities: []string{"test"}, ToolsCount: 7, Status: "running"},
		},
		TotalTools: 11,
	}

	result := FormatForPrompt(cat)

	if !strings.Contains(result, "11 tools total") {
		t.Errorf("expected '11 tools total' in header, output:\n%s", result)
	}
}

func TestFormatForPrompt_EmptyCapabilities(t *testing.T) {
	cat := ServerCatalog{
		Servers: []ServerSummary{
			{
				Name:         "basic",
				Description:  "Basic server",
				Capabilities: nil,
				ToolsCount:   1,
				Status:       "running",
			},
		},
		TotalTools: 1,
	}

	result := FormatForPrompt(cat)

	if !strings.Contains(result, "general") {
		t.Errorf("expected 'general' as fallback for empty capabilities, output:\n%s", result)
	}
}
