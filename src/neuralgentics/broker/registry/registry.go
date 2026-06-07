package registry

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// ServerEntry holds the runtime state of a registered MCP server.
type ServerEntry struct {
	Config  types.ServerConfig
	Process *os.Process
	Tools   []types.ToolSummary
	Stdin   io.WriteCloser
	Stdout  io.ReadCloser
}

// Registry is an in-memory, thread-safe registry mapping server names
// to their configs, processes, and tool lists.
type Registry struct {
	mu      sync.RWMutex
	servers map[string]*ServerEntry
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		servers: make(map[string]*ServerEntry),
	}
}

// Register adds a server entry to the registry. Overwrites any existing
// entry with the same name.
func (r *Registry) Register(config types.ServerConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.servers[config.Name] = &ServerEntry{
		Config: config,
		Tools:  []types.ToolSummary{},
	}
	return nil
}

// Deregister removes a server entry. Returns an error if the server
// is not found.
func (r *Registry) Deregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.servers[name]; !ok {
		return ErrServerNotFound{name: name}
	}
	delete(r.servers, name)
	return nil
}

// Get retrieves a server entry by name.
func (r *Registry) Get(name string) (*ServerEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.servers[name]
	return entry, ok
}

// List returns the status of all registered servers.
func (r *Registry) List() []types.ServerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	statuses := make([]types.ServerStatus, 0, len(r.servers))
	for name, entry := range r.servers {
		running := entry.Process != nil
		statuses = append(statuses, types.ServerStatus{
			Name:    name,
			Running: running,
			Tools:   entry.Tools,
		})
	}
	return statuses
}

// GetTools returns cached tool summaries for a specific server.
func (r *Registry) GetTools(serverName string) []types.ToolSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.servers[serverName]
	if !ok {
		return nil
	}
	return entry.Tools
}

// GetAllTools returns cached tool summaries from all servers.
func (r *Registry) GetAllTools() []types.ToolSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []types.ToolSummary
	for _, entry := range r.servers {
		all = append(all, entry.Tools...)
	}
	return all
}

// UpdateEntry replaces the ServerEntry for a given name. Used by the
// launcher to store process handles and pipes after starting a server.
func (r *Registry) UpdateEntry(name string, entry *ServerEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.servers[name] = entry
}

// ErrServerNotFound is returned when a server name is not in the registry.
type ErrServerNotFound struct {
	name string
}

func (e ErrServerNotFound) Error() string {
	return "server not found: " + e.name
}

// GetServerNames returns a sorted list of all registered server names.
func (r *Registry) GetServerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	return names
}

// InferCapabilities extracts unique capability tags from tool name prefixes.
// Each tool name is split by "_" and the first part is taken as a capability tag.
// Results are deduplicated and maintain insertion order.
func InferCapabilities(tools []types.ToolSummary) []string {
	seen := make(map[string]bool)
	var caps []string
	for _, t := range tools {
		parts := strings.SplitN(t.Name, "_", 2)
		if len(parts) > 0 && parts[0] != "" && !seen[parts[0]] {
			seen[parts[0]] = true
			caps = append(caps, parts[0])
		}
	}
	return caps
}

// RegisterMCPServer adds a multi-transport server configuration to the registry.
// It validates the config, converts the first transport to a legacy ServerConfig,
// and registers it using the existing Register method.
func (r *Registry) RegisterMCPServer(config types.MCPServerConfig) error {
	if config.Name == "" {
		return fmt.Errorf("server name is required")
	}
	if len(config.Transports) == 0 {
		return fmt.Errorf("at least one transport is required")
	}
	sc, err := config.ToLegacyServerConfig()
	if err != nil {
		return fmt.Errorf("convert transport to legacy config: %w", err)
	}
	return r.Register(sc)
}
