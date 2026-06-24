package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/access"
	"neuralgentics-broker/src/neuralgentics/broker/catalog"
	"neuralgentics-broker/src/neuralgentics/broker/intent"
	"neuralgentics-broker/src/neuralgentics/broker/launcher"
	"neuralgentics-broker/src/neuralgentics/broker/profile"
	"neuralgentics-broker/src/neuralgentics/broker/proxy"
	"neuralgentics-broker/src/neuralgentics/broker/registry"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// Broker is the top-level facade for the MCP broker.
// It coordinates the registry, launcher, proxy, and access control
// to manage MCP server lifecycles and route tool calls.
type Broker struct {
	registry      *registry.Registry
	launcher      *launcher.Launcher
	proxy         *proxy.MCPProxy
	access        *access.AccessControl
	builder       *catalog.Builder
	toolExposer   ToolExposer
	httpClients   map[string]proxy.Client // HTTP/SSE clients keyed by server name
	httpMu        sync.RWMutex            // protects httpClients
	WorkspaceRoot string                  // absolute path to project root for skill catalog reads
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
// The WorkspaceRoot is set to the current working directory.
// For a specific workspace root, use NewBrokerWithWorkspace.
func NewBroker() *Broker {
	cwd, _ := os.Getwd()
	return NewBrokerWithWorkspace(cwd)
}

// NewBrokerWithWorkspace creates a new Broker with the given workspace root.
// The workspaceRoot is used for skill catalog reads and other filesystem operations.
func NewBrokerWithWorkspace(workspaceRoot string) *Broker {
	reg := registry.NewRegistry()
	ac := access.NewAccessControl(access.DefaultServerRoles)
	return &Broker{
		registry:      reg,
		launcher:      launcher.NewLauncher(reg),
		proxy:         proxy.NewMCPProxy(),
		access:        ac,
		builder:       catalog.NewBuilderWithAccess(reg, ac),
		httpClients:   make(map[string]proxy.Client),
		WorkspaceRoot: workspaceRoot,
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
// It also cleans up any HTTP client associated with the server.
// The shared proxy is NOT stopped — it is broker-level and used by other servers.
func (b *Broker) DeregisterServer(name string) error {
	// Clean up HTTP client if present.
	b.httpMu.Lock()
	delete(b.httpClients, name)
	b.httpMu.Unlock()

	// Stop the server process (stdout EOF will cause reader to exit).
	_ = b.launcher.Stop(name)

	return b.registry.Deregister(name)
}

// RegisterMCPServer adds a multi-transport server configuration to the registry.
// The first transport in MCPServerConfig.Transports is used as the default
// unless overridden via ActivateMCPServerWithTransport.
func (b *Broker) RegisterMCPServer(config types.MCPServerConfig) error {
	return b.registry.RegisterMCPServer(config)
}

// ActivateMCPServerWithTransport activates a registered MCP server using a
// specific transport from the provided MCPServerConfig. The server must already
// be registered via RegisterServer or RegisterMCPServer. If transportIndex is -1,
// the first transport is used. If the chosen transport fails, the next is tried
// as fallback. Returns the transport.Type that succeeded.
// The caller is responsible for keeping config in sync with what was registered.
func (b *Broker) ActivateMCPServerWithTransport(name string, config types.MCPServerConfig, transportIndex int) (string, error) {
	entry, ok := b.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("server %q not registered", name)
	}
	if len(config.Transports) == 0 {
		return "", fmt.Errorf("server %q has no transports declared", name)
	}

	// Determine start index.
	startIdx := transportIndex
	if startIdx < 0 || startIdx >= len(config.Transports) {
		startIdx = 0
	}

	// Try transports in order, starting at startIdx, wrapping around.
	n := len(config.Transports)
	for i := 0; i < n; i++ {
		idx := (startIdx + i) % n
		transport := config.Transports[idx]

		// Build a temporary ServerConfig for this transport only.
		sc := transport.ToServerConfig(name, config.Description, config.Capabilities)

		// HTTP/SSE transport: use HTTPClient directly.
		if sc.Type == "http" || sc.Type == "sse" {
			client, err := proxy.NewClientForConfig(sc)
			if err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := client.Initialize(ctx); err != nil {
				continue
			}
			tools, err := client.ListTools(ctx)
			if err != nil {
				continue
			}

			// Update registry entry with the chosen config and tools.
			entry.Config = sc
			entry.Tools = tools
			b.registry.UpdateEntry(name, entry)

			// Store the HTTP client for future Call() operations.
			b.httpMu.Lock()
			b.httpClients[name] = client
			b.httpMu.Unlock()

			return string(transport.Type), nil
		}

		// Stdio transport: use the existing subprocess-based activation.
		// Replace entry.Config with the chosen transport's legacy form.
		oldConfig := entry.Config
		entry.Config = sc
		b.registry.UpdateEntry(name, entry)

		// Try to start.
		err := b.StartServer(name)
		if err == nil {
			return string(transport.Type), nil
		}

		// Roll back config on failure so the next iteration starts from a known state.
		entry.Config = oldConfig
		b.registry.UpdateEntry(name, entry)
	}

	return "", fmt.Errorf("all %d transports failed for server %q", n, name)
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
// For HTTP/SSE servers, it uses the stored HTTPClient; for stdio servers,
// it uses the shared MCPProxy.
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

	// Check for an HTTP client first (HTTP/SSE transport).
	b.httpMu.RLock()
	client, isHTTP := b.httpClients[serverName]
	b.httpMu.RUnlock()

	if isHTTP {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return client.Call(ctx, toolName, args)
	}

	// Stdio transport: use the shared proxy.
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

// BuildSkills creates a SkillCatalog filtered by role.
// If role is empty, all skills are included.
// Uses the broker's WorkspaceRoot for filesystem operations.
func (b *Broker) BuildSkills(role string) *catalog.SkillCatalog {
	cat := b.builder.BuildSkills(role, b.WorkspaceRoot)
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

// ─── Curated Catalog Methods (T-CATALOG-001) ─────────────────────────────────────

// DiscoverCatalog returns the list of curated MCP servers not currently registered
// in the broker's registry. If role is non-empty, only servers the role can access
// are returned.
func (b *Broker) DiscoverCatalog(role string) ([]catalog.CuratedServer, error) {
	cat, err := catalog.LoadCuratedCatalog()
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	registered := b.registry.List()
	registeredNames := make(map[string]bool, len(registered))
	for _, s := range registered {
		registeredNames[s.Name] = true
	}
	var available []catalog.CuratedServer
	for _, s := range cat.Servers {
		if registeredNames[s.Name] {
			continue
		}
		if role != "" && !b.access.CanAccess(role, s.Name) {
			continue
		}
		available = append(available, s)
	}
	return available, nil
}

// ActivateFromCatalog registers AND starts a curated MCP server using the
// default transport (or transportIndex if >= 0). The server must NOT already
// be registered. Returns the transport.Type that was activated.
func (b *Broker) ActivateFromCatalog(role, name string, transportIndex int) (string, error) {
	cat, err := catalog.LoadCuratedCatalog()
	if err != nil {
		return "", fmt.Errorf("load catalog: %w", err)
	}
	var found *catalog.CuratedServer
	for i := range cat.Servers {
		if cat.Servers[i].Name == name {
			found = &cat.Servers[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("server %q not found in curated catalog", name)
	}
	if role != "" && !b.access.CanAccess(role, name) {
		return "", access.ErrUnauthorized{Role: role, Server: name, Reason: "role cannot access this catalog server"}
	}
	if len(found.Transports) == 0 {
		return "", fmt.Errorf("server %q has no transports declared", name)
	}

	// Convert curated transports to MCPServerConfig.
	transports := make([]types.TransportConfig, len(found.Transports))
	for i, ct := range found.Transports {
		transports[i] = types.TransportConfig{
			Type:        types.TransportType(ct.Type),
			Package:     ct.Package,
			Args:        ct.Args,
			Env:         ct.Env,
			URL:         ct.URL,
			Default:     ct.Default,
			Description: ct.Description,
		}
	}
	cfg := types.MCPServerConfig{
		Name:         found.Name,
		Transports:   transports,
		Description:  found.Description,
		Capabilities: found.Capabilities,
	}
	if err := b.RegisterMCPServer(cfg); err != nil {
		return "", fmt.Errorf("register %q: %w", name, err)
	}
	return b.ActivateMCPServerWithTransport(name, cfg, transportIndex)
}

// DeactivateMCPServer stops a running server and removes it from the registry.
// Idempotent: returns nil if the server was not registered.
func (b *Broker) DeactivateMCPServer(name string) error {
	if _, ok := b.registry.Get(name); !ok {
		return nil
	}
	return b.DeregisterServer(name)
}

// ListTransports returns the available transports for a curated MCP server,
// annotated with availability based on whether the required binary is on PATH.
func (b *Broker) ListTransports(name string) (transports []catalog.CuratedTransport, unavailable []string, err error) {
	cat, err := catalog.LoadCuratedCatalog()
	if err != nil {
		return nil, nil, fmt.Errorf("load catalog: %w", err)
	}
	for _, s := range cat.Servers {
		if s.Name == name {
			_, unavailable = catalog.CheckTransportAvailability(s.Transports)
			return s.Transports, unavailable, nil
		}
	}
	return nil, nil, fmt.Errorf("server %q not found in curated catalog", name)
}

// ExportProfile collects the current broker state (active provider, active MCPs,
// permissions, opencode snapshot) and writes it to w as a tar.gz profile.
// If passphrase is non-empty, the archive is HMAC-SHA256 signed.
func (b *Broker) ExportProfile(w io.Writer, passphrase, brokerVersion string) error {
	// Get active MCPs from registry.
	statuses := b.registry.List()
	catalogLock := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		entry, ok := b.registry.Get(s.Name)
		if !ok {
			continue
		}
		catalogLock = append(catalogLock, map[string]any{
			"name":    s.Name,
			"running": s.Running,
			"tools":   s.Tools,
			"type":    entry.Config.Type,
			"package": entry.Config.Command,
			"args":    entry.Config.Args,
		})
	}
	catalogJSON, _ := json.Marshal(catalogLock)

	// Get permission matrix snapshot.
	permsJSON, _ := json.Marshal(b.access.Roles())

	// Read provider-pref.json if it exists.
	prefJSON := []byte(`{"activeProvider":"ollama-cloud"}`)
	if home, err := os.UserHomeDir(); err == nil {
		prefPath := filepath.Join(home, ".config", "neuralgentics", "provider-pref.json")
		if data, err := os.ReadFile(prefPath); err == nil {
			prefJSON = data
		}
	}

	// Provider config and opencode snapshot are caller-provided (empty defaults).
	providerJSON := []byte(`{}`)
	opencodeJSON := []byte(`{}`)

	p := profile.Build(providerJSON, catalogJSON, permsJSON, opencodeJSON, prefJSON, brokerVersion)
	return p.Export(w, passphrase)
}

// ImportProfile reads a tar.gz profile from r, verifies signature, and applies
// the active MCPs and provider to the broker. Returns the parsed profile and any
// conflicts (MCPs that already exist in the registry).
func (b *Broker) ImportProfile(r io.Reader, passphrase string) (*profile.Profile, error) {
	p, err := profile.Import(r, passphrase)
	if err != nil {
		return nil, fmt.Errorf("import profile: %w", err)
	}

	// Apply: register each MCP from catalog.lock.
	var catalogLock []map[string]any
	if err := json.Unmarshal(p.Catalog, &catalogLock); err != nil {
		return p, fmt.Errorf("unmarshal catalog.lock: %w", err)
	}
	for _, mcp := range catalogLock {
		name, _ := mcp["name"].(string)
		if name == "" {
			continue
		}
		// If already registered, skip.
		if _, ok := b.registry.Get(name); ok {
			continue
		}
		// Re-register from snapshot.
		cfg := types.ServerConfig{
			Name:    name,
			Command: asString(mcp["package"]),
			Type:    asString(mcp["type"]),
		}
		if args, ok := mcp["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					cfg.Args = append(cfg.Args, s)
				}
			}
		}
		if err := b.RegisterServer(cfg); err != nil {
			// log and continue — don't fail the whole import for one bad MCP.
			continue
		}
	}
	return p, nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
