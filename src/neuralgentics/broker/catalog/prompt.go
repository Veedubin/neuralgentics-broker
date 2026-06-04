package catalog

import (
	"fmt"
	"strings"
)

// FormatForPrompt formats a ServerCatalog as a markdown prompt section
// suitable for inclusion in an LLM system prompt. It produces a markdown
// table of active servers with usage instructions for the broker's
// intent-matching system.
func FormatForPrompt(cat ServerCatalog) string {
	if len(cat.Servers) == 0 {
		return "## Available MCP Servers\n\nNo MCP servers are currently available."
	}

	// Count running servers.
	runningCount := 0
	for _, s := range cat.Servers {
		if s.Status == "running" {
			runningCount++
		}
	}

	var sb strings.Builder

	sb.WriteString("## Available MCP Servers\n\n")
	sb.WriteString("You have access to the following MCP servers through the Neuralgentics broker.\n")
	sb.WriteString("Use the broker.MatchIntent() function to select the right tool by describing\n")
	sb.WriteString("what you want to do. You do NOT need to know tool names — the broker's\n")
	sb.WriteString("intent matcher selects the best tool for you.\n\n")

	sb.WriteString(fmt.Sprintf("### Active Servers (%d running, %d tools total)\n\n", runningCount, cat.TotalTools))

	sb.WriteString("| Server | Description | Capabilities | Tools |\n")
	sb.WriteString("|--------|-------------|--------------|-------|\n")

	for _, s := range cat.Servers {
		caps := strings.Join(s.Capabilities, ", ")
		if caps == "" {
			caps = "general"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d |\n",
			s.Name, s.Description, caps, s.ToolsCount))
	}

	sb.WriteString("\n### How to Use MCP Tools\n\n")
	sb.WriteString("1. **Describe your intent:** Call `broker.MatchIntent(\"search my memories\")`\n")
	sb.WriteString("2. **The broker finds the tool:** Returns `{server: \"...\", tool: \"...\"}`\n")
	sb.WriteString("3. **The broker calls it:** Returns the result directly\n\n")
	sb.WriteString("If you need to see all tools on a specific server, call:\n")
	sb.WriteString("  broker.ExpandServer(\"<server-name>\")\n")

	return sb.String()
}
