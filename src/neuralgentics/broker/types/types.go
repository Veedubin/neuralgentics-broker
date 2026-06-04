package types

// ServerConfig holds the configuration for registering an external MCP server.
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
