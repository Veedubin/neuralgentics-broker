// Package access provides role-based access control for the MCP broker.
// It determines which roles can access which servers.
package access

import (
	"fmt"
	"sort"
	"strings"
)

// Role is a string type representing an agent role in the system.
type Role string

// Role constants define the standard agent roles in the Neuralgentics system.
// These cover both base roles (orchestrator, coder, etc.) and boomerang-*
// agent-specific roles used by the Neuralgentics dispatch pipeline.
const (
	RoleOrchestrator          Role = "orchestrator"
	RoleCoder                 Role = "coder"
	RoleArchitect             Role = "architect"
	RoleExplorer              Role = "explorer"
	RoleTester                Role = "tester"
	RoleWriter                Role = "writer"
	RoleLinter                Role = "linter"
	RoleGit                   Role = "git"
	RoleResearcher            Role = "researcher"
	RoleReviewer              Role = "reviewer"
	RoleBoomerangCoder        Role = "boomerang-coder"
	RoleBoomerangArchitect    Role = "boomerang-architect"
	RoleBoomerangExplorer     Role = "boomerang-explorer"
	RoleBoomerangTester       Role = "boomerang-tester"
	RoleBoomerangLinter       Role = "boomerang-linter"
	RoleBoomerangGit          Role = "boomerang-git"
	RoleBoomerangWriter       Role = "boomerang-writer"
	RoleBoomerangScraper      Role = "boomerang-scraper"
	RoleBoomerangRelease      Role = "boomerang-release"
	RoleBoomerangInit         Role = "boomerang-init"
	RoleBoomerangHandoff      Role = "boomerang-handoff"
	RoleBoomerangAgentBuilder Role = "boomerang-agent-builder"
	RoleMCPSpecialist         Role = "mcp-specialist"
)

// DefaultServerRoles maps server names to the roles that can access them.
// Orchestrator can access all servers by default (enforced in CanAccess).
// Servers with empty role lists are accessible to all roles (allow-all).
// Unregistered servers (not in this map) also default to allow-all.
//
// Permission matrix (derived from AGENTS.md v0.5.0 Agent Permission Overhaul):
//   - memoryManager: allow-all (baseline tool server for every role)
//   - neuralgentics:  allow-all (core dispatch/broker server)
//   - github-mcp:     restricted to boomerang-git only (plus orchestrator wildcard)
//   - playwright:     web automation for tester, scraper, researcher
//   - searxng:        web search for architect, coder, scraper, researcher, linter, git
//   - webfetch:       URL fetch for architect, coder, scraper, researcher, tester, linter, git
//   - websearch:      same policy as webfetch
//   - markitdown:     document conversion for architect, writer, git, release
var DefaultServerRoles = map[string][]Role{
	// Baseline servers — all roles can access (empty list = allow-all).
	"memoryManager": {},
	"neuralgentics": {},

	// Restricted servers — explicit role lists.
	"github-mcp": {RoleBoomerangGit},
	"playwright": {RoleTester, RoleBoomerangTester, RoleBoomerangScraper, RoleResearcher},
	"searxng":    {RoleArchitect, RoleCoder, RoleBoomerangArchitect, RoleBoomerangCoder, RoleBoomerangScraper, RoleResearcher, RoleLinter, RoleBoomerangLinter, RoleGit, RoleBoomerangGit},
	"webfetch":   {RoleArchitect, RoleCoder, RoleBoomerangArchitect, RoleBoomerangCoder, RoleBoomerangScraper, RoleResearcher, RoleTester, RoleBoomerangTester, RoleLinter, RoleBoomerangLinter, RoleGit, RoleBoomerangGit},
	"websearch":  {RoleArchitect, RoleCoder, RoleBoomerangArchitect, RoleBoomerangCoder, RoleBoomerangScraper, RoleResearcher, RoleTester, RoleBoomerangTester, RoleLinter, RoleBoomerangLinter, RoleGit, RoleBoomerangGit},
	"markitdown": {RoleArchitect, RoleBoomerangArchitect, RoleWriter, RoleBoomerangWriter, RoleGit, RoleBoomerangGit, RoleBoomerangRelease},
}

// AccessControl enforces role-based permissions on MCP server access.
type AccessControl struct {
	// serverRoles maps server names to the list of roles that can access them.
	// If a server is not in the map, all roles can access it (default allow).
	// If a server has an empty roles list, all roles can access it.
	serverRoles map[string][]Role
}

// NewAccessControl creates a new AccessControl with a defensive copy of the
// given role mappings.
func NewAccessControl(serverRoles map[string][]Role) *AccessControl {
	// Defensive copy to prevent external mutation.
	copied := make(map[string][]Role, len(serverRoles))
	for k, v := range serverRoles {
		roles := make([]Role, len(v))
		copy(roles, v)
		copied[k] = roles
	}
	return &AccessControl{
		serverRoles: copied,
	}
}

// DefaultAccessControl returns an AccessControl configured with DefaultServerRoles.
func DefaultAccessControl() *AccessControl {
	return NewAccessControl(DefaultServerRoles)
}

// CanAccess checks whether a role is permitted to access a given server.
// Orchestrator role always has access. If a server has no explicit role
// mapping (unregistered), all roles can access it (default allow).
// If a server has an empty roles list, all roles can access it.
func (ac *AccessControl) CanAccess(role string, serverName string) bool {
	// Orchestrator always has access.
	if role == string(RoleOrchestrator) {
		return true
	}

	roles, exists := ac.serverRoles[serverName]
	if !exists {
		// Not in the map → default allow.
		return true
	}

	if len(roles) == 0 {
		// Empty roles list → allow all.
		return true
	}

	for _, r := range roles {
		if string(r) == role {
			return true
		}
	}
	return false
}

// GetAccessibleServers returns a sorted list of server names accessible by
// the given role. Orchestrator gets all servers in the role map.
// For servers with empty role lists (allow-all), the server is included
// in the result for every role.
func (ac *AccessControl) GetAccessibleServers(role string) []string {
	if role == string(RoleOrchestrator) {
		names := make([]string, 0, len(ac.serverRoles))
		for name := range ac.serverRoles {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}

	var accessible []string
	for name, roles := range ac.serverRoles {
		if len(roles) == 0 {
			// Empty list → allow-all: this server is accessible to every role.
			accessible = append(accessible, name)
			continue
		}
		for _, r := range roles {
			if string(r) == role {
				accessible = append(accessible, name)
				break
			}
		}
	}
	sort.Strings(accessible)
	return accessible
}

// Grant adds a role to the allowed list for a server.
// If the server currently has an empty role list (allow-all), this operation
// replaces it with a list containing only the granted role, making the server
// restricted to that role (plus orchestrator). To avoid accidental lockdown,
// call Grant only on servers that already have a non-empty role list, or
// explicitly set the initial role list via NewAccessControl.
func (ac *AccessControl) Grant(role string, serverName string) {
	roles, exists := ac.serverRoles[serverName]
	if !exists {
		// Server not in map (default allow). Adding an explicit list
		// restricts access to only the granted role + orchestrator wildcard.
		ac.serverRoles[serverName] = []Role{Role(role)}
		return
	}
	// Check if already granted.
	for _, r := range roles {
		if string(r) == role {
			return // Already present.
		}
	}
	ac.serverRoles[serverName] = append(roles, Role(role))
}

// Revoke removes a role from the allowed list for a server.
// If removing the last role from a server's list, the server becomes
// inaccessible to all non-orchestrator roles (empty non-nil list).
// To make a server allow-all again, re-register it with an empty list
// via NewAccessControl or by setting ac.serverRoles[serverName] = []Role{}.
func (ac *AccessControl) Revoke(role string, serverName string) {
	roles, exists := ac.serverRoles[serverName]
	if !exists {
		return // Not in map (default allow), nothing to revoke.
	}
	filtered := make([]Role, 0, len(roles))
	for _, r := range roles {
		if string(r) != role {
			filtered = append(filtered, r)
		}
	}
	ac.serverRoles[serverName] = filtered
}

// ServerNames returns a sorted list of all registered server names.
func (ac *AccessControl) ServerNames() []string {
	names := make([]string, 0, len(ac.serverRoles))
	for name := range ac.serverRoles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Roles returns a defensive copy of the internal serverRoles map,
// converted to string keys and string slice values for JSON serialization.
// This is used by the profile export to snapshot the permission matrix.
func (ac *AccessControl) Roles() map[string][]string {
	result := make(map[string][]string, len(ac.serverRoles))
	for server, roles := range ac.serverRoles {
		rs := make([]string, len(roles))
		for i, r := range roles {
			rs[i] = string(r)
		}
		result[server] = rs
	}
	return result
}

// AllRoles returns all Role constants defined in this package.
func AllRoles() []Role {
	return []Role{
		RoleOrchestrator,
		RoleCoder,
		RoleArchitect,
		RoleExplorer,
		RoleTester,
		RoleWriter,
		RoleLinter,
		RoleGit,
		RoleResearcher,
		RoleReviewer,
		RoleBoomerangCoder,
		RoleBoomerangArchitect,
		RoleBoomerangExplorer,
		RoleBoomerangTester,
		RoleBoomerangLinter,
		RoleBoomerangGit,
		RoleBoomerangWriter,
		RoleBoomerangScraper,
		RoleBoomerangRelease,
		RoleBoomerangInit,
		RoleBoomerangHandoff,
		RoleBoomerangAgentBuilder,
		RoleMCPSpecialist,
	}
}

// ErrUnauthorized is returned when a role does not have access to a server.
type ErrUnauthorized struct {
	Role             string
	Server           string
	Reason           string
	AvailableServers []string
}

func (e ErrUnauthorized) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("unauthorized: %s", e.Reason)
	}
	return fmt.Sprintf("unauthorized: role %q cannot access server %q", e.Role, e.Server)
}

// ToJSONError returns a JSON-RPC compatible error structure.
func (e ErrUnauthorized) ToJSONError() map[string]any {
	suggestion := fmt.Sprintf("%s can access: %s. Use MatchIntent() for alternatives.",
		e.Role, strings.Join(e.AvailableServers, ", "))

	return map[string]any{
		"error": map[string]any{
			"code":    -32001,
			"message": e.Error(),
			"data": map[string]any{
				"available_servers": e.AvailableServers,
				"suggestion":        suggestion,
			},
		},
	}
}
