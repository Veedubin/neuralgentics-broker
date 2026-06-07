package types

import (
	"fmt"
	"os/exec"
)

// ServerConfig holds the configuration for registering an external MCP server.
// This is the legacy single-transport form. For multi-transport support, use
// MCPServerConfig instead.
type ServerConfig struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Type         string   // "stdio", "http", "sse"
	Description  string   // Human-readable description of the server
	Capabilities []string // Capability tags (e.g., "filesystem", "memory")
}

// ToolSummary is a minimal tool summary for token-efficient listing.
// Returns Name + Description only. No full JSON schemas.
type ToolSummary struct {
	Server      string
	Name        string
	Description string
}

// ServerStatus represents the current state of a registered MCP server.
type ServerStatus struct {
	Name      string
	Running   bool
	Tools     []ToolSummary
	LastError string
}

// ─── Multi-Transport Types (T-TRANSPORT-ABSTRACTION) ─────────────────────────

// TransportType identifies how an MCP server process is launched.
type TransportType string

const (
	// TransportNPX spawns an MCP via `npx -y <package>` (Node.js ecosystem).
	TransportNPX TransportType = "npx"
	// TransportUVX spawns an MCP via `uvx <package>` (Python ecosystem).
	TransportUVX TransportType = "uvx"
	// TransportLocal spawns an MCP via a local binary path (e.g. ~/.local/bin/foo).
	TransportLocal TransportType = "local"
	// TransportDocker spawns an MCP via `docker run -i --rm <image>` or podman equivalent.
	TransportDocker TransportType = "docker"
	// TransportHTTP connects to a remote MCP server over HTTP (e.g. hosted MCP).
	TransportHTTP TransportType = "http"
)

// TransportConfig declares a single transport option for an MCP server.
// One MCPServerConfig contains a list of these (in priority order).
type TransportConfig struct {
	Type        TransportType     // Required: npx, uvx, local, docker, http
	Package     string            // For npx/uvx: the package name. For docker: the image. For local: the binary path. Empty for http.
	Args        []string          // Extra args passed after Package (e.g. ["-y", "--prefix", "/opt"]).
	Env         map[string]string // Environment variables.
	URL         string            // For http transport only.
	Default     bool              // If true, this transport is the preferred choice when no override is given.
	Description string            // Human-readable explanation (e.g. "Run via NPX (Node.js 18+ required)").
}

// MCPServerConfig is the new multi-transport form of ServerConfig.
// It supports fallback chains: if the default transport fails to launch,
// the broker tries the next one in the list.
type MCPServerConfig struct {
	Name         string
	Transports   []TransportConfig // Ordered list, first = default
	Description  string
	Capabilities []string
}

// ToLegacyServerConfig returns the highest-priority transport as a legacy
// ServerConfig, used by the existing single-transport code paths.
// Returns an error if Transports is empty.
func (c *MCPServerConfig) ToLegacyServerConfig() (ServerConfig, error) {
	if len(c.Transports) == 0 {
		return ServerConfig{}, fmt.Errorf("MCPServerConfig %q has no transports", c.Name)
	}
	t := c.Transports[0]
	return t.ToServerConfig(c.Name, c.Description, c.Capabilities), nil
}

// ToServerConfig converts a single TransportConfig to the legacy single-transport
// ServerConfig form, filling in name/description/capabilities from the parent.
func (t TransportConfig) ToServerConfig(name, description string, capabilities []string) ServerConfig {
	sc := ServerConfig{
		Name:         name,
		Env:          t.Env,
		Description:  description,
		Capabilities: capabilities,
	}
	switch t.Type {
	case TransportNPX:
		sc.Command = "npx"
		sc.Args = append([]string{"-y", t.Package}, t.Args...)
		sc.Type = "stdio"
	case TransportUVX:
		sc.Command = "uvx"
		sc.Args = append([]string{t.Package}, t.Args...)
		sc.Type = "stdio"
	case TransportLocal:
		sc.Command = t.Package // binary path
		sc.Args = t.Args
		sc.Type = "stdio"
	case TransportDocker:
		sc.Command = detectContainerRuntime() // "docker" or "podman"
		sc.Args = append([]string{"run", "-i", "--rm"}, t.Args...)
		if t.Package != "" {
			sc.Args = append(sc.Args, t.Package)
		}
		sc.Type = "stdio"
	case TransportHTTP:
		sc.Command = "" // not used for http
		sc.Type = "http"
		// URL is exposed via the proxy layer (out of scope here, just store as env hint)
		if t.URL != "" {
			if sc.Env == nil {
				sc.Env = map[string]string{}
			}
			sc.Env["NEURALGENTICS_MCP_URL"] = t.URL
		}
	}
	return sc
}

// detectContainerRuntime returns "docker" if available, otherwise "podman".
// Returns "docker" if neither is found (the launch will then fail with a clear error).
func detectContainerRuntime() string {
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	return "docker" // Will fail at exec time with "executable file not found"
}
