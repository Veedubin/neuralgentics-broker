// Package catalog builds and manages server and tool catalogs for the
// Neuralgentics MCP broker. It constructs token-efficient summaries of
// available servers and their tools, with role-based filtering and
// automatic capability inference.
package catalog

import (
	"fmt"
	"strings"

	"neuralgentics-broker/src/neuralgentics/broker/access"
	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// ServerSummary is a lightweight description of an MCP server for catalog display.
type ServerSummary struct {
	Name         string
	Description  string
	Capabilities []string
	ToolsCount   int
	Status       string // "running", "stopped", "registered"
}

// ServerCatalog is an aggregate view of all available servers, filtered by role.
type ServerCatalog struct {
	Servers    []ServerSummary
	TotalTools int
	Role       string // The role this catalog was built for (empty = all)
}

// ToolCatalog is a detailed listing of tools for a specific server.
type ToolCatalog struct {
	Server string
	Status string
	Tools  []ToolSummary
}

// ToolSummary describes a single tool within a ToolCatalog.
type ToolSummary struct {
	Name        string
	Description string
}

// Builder constructs ServerCatalogs from the registry, with optional
// role-based access filtering.
type Builder struct {
	registry *registry.Registry
	ac       *access.AccessControl
}

// NewBuilder creates a new catalog Builder backed by the given registry.
// It uses DefaultAccessControl for role-based filtering.
func NewBuilder(reg *registry.Registry) *Builder {
	return &Builder{
		registry: reg,
		ac:       access.DefaultAccessControl(),
	}
}

// NewBuilderWithAccess creates a catalog Builder with a custom AccessControl.
func NewBuilderWithAccess(reg *registry.Registry, ac *access.AccessControl) *Builder {
	return &Builder{
		registry: reg,
		ac:       ac,
	}
}

// Build creates a ServerCatalog filtered by role.
// If role is empty, all servers are included.
func (b *Builder) Build(role string) ServerCatalog {
	statuses := b.registry.List()
	var summaries []ServerSummary
	totalTools := 0

	for _, status := range statuses {
		// Filter by role-based access control.
		if role != "" && !b.ac.CanAccess(role, status.Name) {
			continue
		}

		entry, ok := b.registry.Get(status.Name)
		if !ok {
			continue
		}

		summary := buildSummary(entry)
		totalTools += summary.ToolsCount
		summaries = append(summaries, summary)
	}

	return ServerCatalog{
		Servers:    summaries,
		TotalTools: totalTools,
		Role:       role,
	}
}

// ExpandServer creates a ToolCatalog for a specific server, expanding
// all its tools with full descriptions.
func (b *Builder) ExpandServer(serverName string) (ToolCatalog, error) {
	entry, ok := b.registry.Get(serverName)
	if !ok {
		return ToolCatalog{}, fmt.Errorf("server %q not found", serverName)
	}

	st := "registered"
	if entry.Process != nil {
		st = "running"
	} else if entry.Config.Command != "" {
		st = "stopped"
	}

	tools := make([]ToolSummary, len(entry.Tools))
	for i, t := range entry.Tools {
		tools[i] = ToolSummary{
			Name:        t.Name,
			Description: t.Description,
		}
	}

	return ToolCatalog{
		Server: serverName,
		Status: st,
		Tools:  tools,
	}, nil
}

// buildSummary creates a ServerSummary from a ServerEntry.
func buildSummary(entry *registry.ServerEntry) ServerSummary {
	// Determine status.
	st := "registered"
	if entry.Process != nil {
		st = "running"
	} else if entry.Config.Command != "" {
		st = "stopped"
	}

	// Determine description with fallback.
	desc := entry.Config.Description
	if desc == "" && len(entry.Tools) > 0 {
		desc = truncate(entry.Tools[0].Description, 80)
	}

	// Determine capabilities with fallback.
	var caps []string
	if len(entry.Config.Capabilities) > 0 {
		caps = make([]string, len(entry.Config.Capabilities))
		copy(caps, entry.Config.Capabilities)
	}
	if len(caps) == 0 && len(entry.Tools) > 0 {
		caps = inferCapabilities(entry.Tools)
	}

	return ServerSummary{
		Name:         entry.Config.Name,
		Description:  desc,
		Capabilities: caps,
		ToolsCount:   len(entry.Tools),
		Status:       st,
	}
}

// inferCapabilities extracts unique capability tags from tool name prefixes.
// Each tool name is split by "_" and the first part is taken as a capability tag.
// Results are deduplicated and maintain insertion order.
func inferCapabilities(tools []types.ToolSummary) []string {
	seen := make(map[string]bool)
	var caps []string
	for _, tool := range tools {
		parts := strings.SplitN(tool.Name, "_", 2)
		prefix := parts[0]
		if prefix != "" && !seen[prefix] {
			seen[prefix] = true
			caps = append(caps, prefix)
		}
	}
	return caps
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
// It attempts to break at a word boundary (space) when possible.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}

	// Try to break at a word boundary.
	cutAt := maxLen - 3
	lastSpace := strings.LastIndex(s[:cutAt], " ")
	if lastSpace > 0 {
		return s[:lastSpace] + " ..."
	}
	return s[:cutAt] + "..."
}
