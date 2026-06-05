package broker

import (
	"context"
	"fmt"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/access"
	"neuralgentics-broker/src/neuralgentics/broker/catalog"
	"neuralgentics-broker/src/neuralgentics/broker/intent"
	"neuralgentics-broker/src/neuralgentics/broker/launcher"
	"neuralgentics-broker/src/neuralgentics/broker/proxy"
	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// Broker is the top-level facade for the MCP broker.
// It coordinates the registry, launcher, proxy, and access control
// to manage MCP server lifecycles and route tool calls.
type Broker struct {
	registry    *registry.Registry
	launcher    *launcher.Launcher
	proxy       *proxy.MCPProxy
	access      *access.AccessControl
	builder     *catalog.Builder
	toolExposer ToolExposer
}

// ToolExposer is an interface for checking and recording tool exposure
// for lazy/demand-driven tool expansion. The MemorySystem implements this
// to track which tools each agent has been exposed to.
type ToolExposer interface {
	RecordToolRequest(peerID, toolServer, toolName string) error
	IncrementToolUse(peerID, toolServer, toolName string) (bool, error)
	GetAgentTools(peerID string) ([]ToolExposure, error)
}

// ToolExposure represents a single tool exposure record for an agent.
type ToolExposure struct {
	ToolServer   string
	ToolName     string
	UseCount     int
	BypassBroker bool
}

// NewBroker creates a new Broker with an empty registry, launcher, proxy,
// and default access control with DefaultServerRoles.
func NewBroker() *Broker {
	reg := registry.NewRegistry()
	ac := access.NewAccessControl(access.DefaultServerRoles)
	return &Broker{
		registry: reg,
		launcher: launcher.NewLauncher(reg),
		proxy:    proxy.NewMCPProxy(),
		access:   ac,
		builder:  catalog.NewBuilderWithAccess(reg, ac),
	}
}

// SetToolExposer sets the tool exposer for lazy tool exposure tracking.
// The MemorySystem implements the ToolExposer interface.
func (b *Broker) SetToolExposer(exposer ToolExposer) {
	b.toolExposer = exposer
}

// RegisterServer adds a server configuration to the registry.
func (b *Broker) RegisterServer(config types.ServerConfig) error {
	if config.Name == "" {
		return fmt.Errorf("server name is required")
	}
	if config.Command == "" {
		return fmt.Errorf("server command is required")
	}
	return b.registry.Register(config)
}

// StartServer launches a registered server, initializes the MCP handshake,
// and discovers available tools. The server must be registered first via
// RegisterServer(). This orchestrates the full lifecycle:
//  1. Launch the subprocess via launcher.Start
//  2. Start the async reader on the server's stdout
//  3. Perform the MCP initialize/initialized handshake
//  4. Discover tools via tools/list
//  5. Store discovered tools in the registry entry
func (b *Broker) StartServer(name string) error {
	entry, ok := b.registry.Get(name)
	if !ok {
		return fmt.Errorf("server %q not registered", name)
	}

	// Step 1: Launch the server subprocess.
	if err := b.launcher.Start(entry.Config); err != nil {
		return fmt.Errorf("start server %q: %w", name, err)
	}

	// Refresh entry to get Stdin/Stdout pipes set by launcher.
	entry, ok = b.registry.Get(name)
	if !ok {
		return fmt.Errorf("server %q disappeared after start", name)
	}
	if entry.Stdin == nil || entry.Stdout == nil {
		return fmt.Errorf("server %q has no stdio pipes after start", name)
	}

	// Step 2: Start async reader on stdout.
	b.proxy.StartReader(entry.Stdout)

	// Step 3: MCP initialize handshake.
	if err := b.proxy.Initialize(name, entry.Stdin, entry.Stdout); err != nil {
		b.proxy.Stop()
		_ = b.launcher.Stop(name)
		return fmt.Errorf("initialize server %q: %w", name, err)
	}

	// Step 4: Discover tools.
	tools, err := b.proxy.ListTools(name, entry.Stdin, entry.Stdout)
	if err != nil {
		b.proxy.Stop()
		_ = b.launcher.Stop(name)
		return fmt.Errorf("list tools from %q: %w", name, err)
	}

	// Step 5: Store tools in registry entry.
	entry.Tools = tools
	b.registry.UpdateEntry(name, entry)

	return nil
}

// ReloadServer stops a running server and restarts it with the same config.
// If the server is registered but not running, it simply starts it.
// Returns an error if the server is not registered or if the restart fails.
// The server remains in the registry throughout the reload cycle.
func (b *Broker) ReloadServer(name string) error {
	entry, ok := b.registry.Get(name)
	if !ok {
		return fmt.Errorf("server %q not registered", name)
	}

	// Idempotent: if not running, just start it.
	if entry.Process == nil {
		return b.StartServer(name)
	}

	// Stop the async proxy reader.
	b.proxy.Stop()

	// Stop the server process (without removing from registry).
	if err := b.launcher.Stop(name); err != nil {
		return fmt.Errorf("stop server %q for reload: %w", name, err)
	}

	// Brief pause to let the OS release resources (ports, sockets).
	time.Sleep(100 * time.Millisecond)

	// Re-launch with the same config.
	if err := b.StartServer(name); err != nil {
		return fmt.Errorf("restart server %q after reload: %w", name, err)
	}

	return nil
}

// ReloadServerWithConfig stops a running server, updates its configuration
// (environment variables and/or CLI arguments), and restarts it with the new
// config. If the server is registered but not running, it starts it with the
// new config (idempotent). The server remains in the registry throughout.
// Only the fields present in newConfig are applied; caller should copy over
// any unchanged fields from the original config to preserve them.
func (b *Broker) ReloadServerWithConfig(name string, newConfig types.ServerConfig) error {
	entry, ok := b.registry.Get(name)
	if !ok {
		return fmt.Errorf("server %q not registered", name)
	}

	// Merge: replace entry.Config with the new config, preserving the name
	// in case the caller omitted it.
	newConfig.Name = name
	entry.Config = newConfig
	b.registry.UpdateEntry(name, entry)

	// Idempotent: if not running, just start it with the new config.
	if entry.Process == nil {
		return b.StartServer(name)
	}

	// Stop the async proxy reader.
	b.proxy.Stop()

	// Stop the server process (without removing from registry).
	if err := b.launcher.Stop(name); err != nil {
		return fmt.Errorf("stop server %q for config reload: %w", name, err)
	}

	// Brief pause to let the OS release resources (ports, sockets).
	time.Sleep(100 * time.Millisecond)

	// Re-launch with the updated config.
	if err := b.StartServer(name); err != nil {
		return fmt.Errorf("restart server %q after config reload: %w", name, err)
	}

	return nil
}

// DeregisterServer removes a server from the registry and stops it if running.
// It does NOT stop the shared proxy — the proxy is broker-level, not server-level,
// and stopping it would kill the async reader for all other running servers.
// The server process is stopped via launcher, and its stdout will EOF naturally,
// causing the readLoop goroutine to exit cleanly.
func (b *Broker) DeregisterServer(name string) error {
	// Stop the server process (stdout EOF will cause reader to exit).
	_ = b.launcher.Stop(name)

	return b.registry.Deregister(name)
}

// ListServers returns the status of all registered servers.
func (b *Broker) ListServers() []types.ServerStatus {
	return b.registry.List()
}

// ListTools returns tool summaries from all registered servers.
func (b *Broker) ListTools() []types.ToolSummary {
	return b.registry.GetAllTools()
}

// Health returns a map of server names to their health status string.
// Possible statuses: "stopped", "dead", "initializing", "healthy", "unhealthy".
// This is a lightweight check that does not send any JSON-RPC requests.
func (b *Broker) Health() map[string]string {
	statuses := b.registry.List()
	health := make(map[string]string, len(statuses))
	for _, s := range statuses {
		if !s.Running {
			health[s.Name] = string(launcher.HealthStopped)
			continue
		}
		health[s.Name] = string(b.launcher.Health(s.Name))
	}
	return health
}

// HealthDeep performs a deeper health check for all running servers.
// For each running server, it attempts a lightweight JSON-RPC handshake
// (initialize) with a 2-second timeout. If the server responds, it
// reports "healthy"; otherwise it reports "unhealthy".
// Non-running servers delegate to Health() status.
func (b *Broker) HealthDeep() map[string]string {
	statuses := b.registry.List()
	health := make(map[string]string, len(statuses))
	for _, s := range statuses {
		if !s.Running {
			health[s.Name] = string(launcher.HealthStopped)
			continue
		}

		entry, ok := b.registry.Get(s.Name)
		if !ok || entry.Process == nil {
			health[s.Name] = string(launcher.HealthStopped)
			continue
		}

		// No pipes — cannot send a ping.
		if entry.Stdin == nil || entry.Stdout == nil {
			health[s.Name] = string(b.launcher.Health(s.Name))
			continue
		}

		// Attempt a lightweight "ping" via JSON-RPC initialize with a 2-second deadline.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		type pingResult struct {
			err error
		}
		ch := make(chan pingResult, 1)
		go func() {
			err := b.proxy.Initialize(s.Name, entry.Stdin, entry.Stdout)
			ch <- pingResult{err: err}
		}()

		select {
		case res := <-ch:
			if res.err != nil {
				health[s.Name] = string(launcher.HealthUnhealthy)
			} else {
				health[s.Name] = string(launcher.HealthHealthy)
			}
		case <-ctx.Done():
			health[s.Name] = string(launcher.HealthUnhealthy)
		}
		cancel()
	}
	return health
}

// Call sends a JSON-RPC tool call to a registered server.
// It checks access control first; if the role does not have permission,
// it returns access.ErrUnauthorized with available server hints.
func (b *Broker) Call(role string, serverName string, toolName string, args map[string]any) (map[string]any, error) {
	// Check access control.
	if !b.access.CanAccess(role, serverName) {
		accessible := b.access.GetAccessibleServers(role)
		return nil, access.ErrUnauthorized{
			Role:             role,
			Server:           serverName,
			Reason:           fmt.Sprintf("role %s cannot access server %s", role, serverName),
			AvailableServers: accessible,
		}
	}

	entry, ok := b.registry.Get(serverName)
	if !ok {
		return nil, fmt.Errorf("server %q not found", serverName)
	}
	if entry.Stdin == nil || entry.Stdout == nil {
		return nil, fmt.Errorf("server %q is not running (no pipes)", serverName)
	}

	params := map[string]any{
		"name":      toolName,
		"arguments": args,
	}

	result, err := b.proxy.Call(serverName, "tools/call", params, entry.Stdin, entry.Stdout)
	if err != nil {
		return nil, fmt.Errorf("call %q on %q: %w", toolName, serverName, err)
	}

	return result, nil
}

// MatchIntent finds the best matching tool for a natural language intent.
// If role is non-empty, only tools from servers accessible to that role
// are considered.
func (b *Broker) MatchIntent(role string, intentStr string) (*intent.ToolMatch, error) {
	allTools := b.registry.GetAllTools()

	// Filter tools by role-based access control if role is specified.
	if role != "" {
		var filtered []types.ToolSummary
		for _, tool := range allTools {
			if b.access.CanAccess(role, tool.Server) {
				filtered = append(filtered, tool)
			}
		}
		allTools = filtered
	}

	if len(allTools) == 0 {
		return nil, fmt.Errorf("no tools available for intent matching")
	}

	matcher := intent.NewMatcher(allTools)
	return matcher.Match(intentStr)
}

// BuildServerCatalog creates a ServerCatalog filtered by role.
// If role is empty, all servers are included.
func (b *Broker) BuildServerCatalog(role string) *catalog.ServerCatalog {
	cat := b.builder.Build(role)
	return &cat
}

// ExpandServer returns a ToolCatalog with all tools for a specific server.
func (b *Broker) ExpandServer(name string) (*catalog.ToolCatalog, error) {
	tc, err := b.builder.ExpandServer(name)
	if err != nil {
		return nil, err
	}
	return &tc, nil
}

// InjectPrompt returns a formatted prompt section for the LLM system prompt.
// It builds the server catalog for the given role and formats it.
func (b *Broker) InjectPrompt(role string) (string, error) {
	cat := b.builder.Build(role)
	return catalog.FormatForPrompt(cat), nil
}

// AccessControl returns the broker's access control for external inspection.
func (b *Broker) AccessControl() *access.AccessControl {
	return b.access
}

// RegisterToolRequest records that a peer/agent has requested access to a tool.
// If the tool exposer is not set (no MemorySystem wired), it returns nil.
// If the peer has already been exposed to this tool, the request is silently
// ignored (INSERT ON CONFLICT DO NOTHING).
func (b *Broker) RegisterToolRequest(peerID, toolServer, toolName string) (bool, error) {
	if b.toolExposer == nil {
		return false, nil
	}
	if err := b.toolExposer.RecordToolRequest(peerID, toolServer, toolName); err != nil {
		return false, fmt.Errorf("record tool request: %w", err)
	}
	return true, nil
}

// IncrementToolUseCount increments the use count for a peer's tool and returns
// whether the tool has reached the bypass threshold (use_count >= 5).
// If the tool exposer is not set, it returns false, nil.
func (b *Broker) IncrementToolUseCount(peerID, toolServer, toolName string) (bool, error) {
	if b.toolExposer == nil {
		return false, nil
	}
	return b.toolExposer.IncrementToolUse(peerID, toolServer, toolName)
}

// GetDemandExpandedTools returns the list of tools a peer has been exposed
// to via demand-driven expansion (beyond the core default set).
func (b *Broker) GetDemandExpandedTools(peerID string) ([]ToolExposure, error) {
	if b.toolExposer == nil {
		return nil, nil
	}
	return b.toolExposer.GetAgentTools(peerID)
}

// SetTools manually sets tool summaries for a server (for testing/demo purposes).
// In production, tools are populated via Initialize + ListTools.
func (b *Broker) SetTools(serverName string, tools []types.ToolSummary) {
	entry, ok := b.registry.Get(serverName)
	if !ok {
		return
	}
	entry.Tools = tools
	b.registry.UpdateEntry(serverName, entry)
}
