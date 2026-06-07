package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"neuralgentics-broker/src/neuralgentics/broker/launcher"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

// jsonrpcRequest represents a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcError represents a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcNotification represents a JSON-RPC notification (no id field).
type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rawMessage is used to detect whether an incoming line has an "id" field.
type rawMessage struct {
	ID *int64 `json:"id"`
}

// NotificationHandler is called when a notification (no id) arrives from a server.
type NotificationHandler func(method string, params json.RawMessage)

// MCPProxy manages JSON-RPC communication with MCP servers over stdio.
// It supports an async reader goroutine that routes responses and notifications.
type MCPProxy struct {
	mu         sync.Mutex
	nextID     int64
	pending    map[int64]chan *jsonrpcResponse
	stdin      io.Writer
	writeMu    sync.Mutex // protects stdin writes for concurrent access
	readerCh   chan struct{}
	readerDone chan struct{}
	running    bool
	onNotify   NotificationHandler
}

// NewMCPProxy creates a new MCP proxy for JSON-RPC communication.
func NewMCPProxy() *MCPProxy {
	return &MCPProxy{
		nextID:     1,
		pending:    make(map[int64]chan *jsonrpcResponse),
		readerDone: make(chan struct{}),
	}
}

// StartReader launches a background goroutine that continuously reads lines
// from stdout and routes them to the correct pending request (by id) or
// to the notification handler (lines without an id field).
// This must be called before sendRPC when using async mode.
func (p *MCPProxy) StartReader(stdout io.Reader) {
	p.mu.Lock()
	if p.readerCh != nil {
		// Already running — close old channel first.
		close(p.readerCh)
	}
	p.readerCh = make(chan struct{})
	p.readerDone = make(chan struct{})
	p.running = true
	p.mu.Unlock()

	go func() {
		p.readLoop(stdout)
		close(p.readerDone)
	}()
}

// readLoop continuously scans lines from stdout, classifying each as a
// response (has id) or a notification (no id), and routes accordingly.
// It does not hold the mutex while delivering to channels — only briefly
// for map lookups.
func (p *MCPProxy) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		// Check if we should stop (non-blocking).
		select {
		case <-p.readerCh:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Determine if this is a response (has id) or a notification (no id).
		var raw rawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			// Malformed line — skip it.
			continue
		}

		if raw.ID != nil {
			// It's a response — route to the pending channel.
			var resp jsonrpcResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			p.mu.Lock()
			ch, ok := p.pending[resp.ID]
			p.mu.Unlock()
			if ok {
				ch <- &resp
			}
		} else {
			// It's a notification — invoke handler if set.
			p.mu.Lock()
			handler := p.onNotify
			p.mu.Unlock()
			if handler != nil {
				var notif jsonrpcNotification
				if err := json.Unmarshal(line, &notif); err != nil {
					continue
				}
				handler(notif.Method, notif.Params)
			}
		}
	}

	// Scanner stopped — signal that reader is done.
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

// SetNotificationHandler registers a callback for MCP notifications
// (messages without an id field, such as "notifications/initialized").
func (p *MCPProxy) SetNotificationHandler(handler NotificationHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onNotify = handler
}

// Stop signals the reader goroutine to exit and cleans up pending entries.
// It closes all pending response channels so waiting sendRPC calls return errors.
func (p *MCPProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.readerCh != nil {
		close(p.readerCh)
		p.readerCh = nil
	}
	p.running = false

	// Close all pending channels so waiting sendRPC calls return errors.
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
}

// Initialize sends the MCP initialize handshake to a server.
// In async mode, StartReader must be called first.
func (p *MCPProxy) Initialize(serverName string, stdin io.Writer, stdout io.Reader) error {
	p.mu.Lock()
	p.stdin = stdin
	p.mu.Unlock()

	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "neuralgentics-broker",
			"version": "0.1.0",
		},
	}

	resp, err := p.sendRPC(stdin, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize handshake for %q: %w", serverName, err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error from %q: code=%d message=%s",
			serverName, resp.Error.Code, resp.Error.Message)
	}

	// Send initialized notification (no ID, fire-and-forget).
	notif := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal initialized notification: %w", err)
	}
	data = append(data, '\n')

	p.writeMu.Lock()
	_, err = stdin.Write(data)
	p.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write initialized notification: %w", err)
	}

	return nil
}

// Call sends a JSON-RPC method call to a server and returns the result.
func (p *MCPProxy) Call(serverName string, method string, params map[string]any, stdin io.Writer, stdout io.Reader) (map[string]any, error) {
	resp, err := p.sendRPC(stdin, method, params)
	if err != nil {
		return nil, fmt.Errorf("call %q on %q: %w", method, serverName, err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("error from %q: code=%d message=%s",
			serverName, resp.Error.Code, resp.Error.Message)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result from %q: %w", serverName, err)
	}

	return result, nil
}

// ListTools requests the tool list from a server and returns summaries.
func (p *MCPProxy) ListTools(serverName string, stdin io.Writer, stdout io.Reader) ([]types.ToolSummary, error) {
	params := map[string]any{}
	resp, err := p.sendRPC(stdin, "tools/list", params)
	if err != nil {
		return nil, fmt.Errorf("list tools from %q: %w", serverName, err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools error from %q: code=%d message=%s",
			serverName, resp.Error.Code, resp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools from %q: %w", serverName, err)
	}

	summaries := make([]types.ToolSummary, 0, len(result.Tools))
	for _, t := range result.Tools {
		summaries = append(summaries, types.ToolSummary{
			Server:      serverName,
			Name:        t.Name,
			Description: t.Description,
		})
	}

	return summaries, nil
}

// sendRPC sends a JSON-RPC request and waits for the response via the
// async reader goroutine. StartReader must be called before sendRPC.
func (p *MCPProxy) sendRPC(stdin io.Writer, method string, params map[string]any) (*jsonrpcResponse, error) {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil, fmt.Errorf("proxy not started, call StartReader first")
	}
	id := p.nextID
	p.nextID++
	// Register pending channel before writing to ensure the reader
	// goroutine can route the response as soon as it arrives.
	ch := make(chan *jsonrpcResponse, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	// Write request — use writeMu to serialize stdin writes across concurrent calls.
	p.writeMu.Lock()
	_, err = stdin.Write(data)
	p.writeMu.Unlock()
	if err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait on the pending channel for the reader goroutine to route the response.
	select {
	case resp, ok := <-ch:
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("proxy stopped while waiting for response to %q", method)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response to %q (id=%d)", method, id)
	}
}

// ─── Client Interface and Dispatch (T-HTTP-TRANSPORT) ─────────────────────────

// Client is the interface that both MCPProxy (stdio) and HTTPClient (HTTP/SSE) implement.
// It exposes the methods the broker needs to manage a server's lifecycle and route calls.
type Client interface {
	// Initialize performs the MCP handshake (initialize + initialized for stdio,
	// or initialize over HTTP for remote servers).
	Initialize(ctx context.Context) error
	// ListTools requests the tool list from the server.
	ListTools(ctx context.Context) ([]types.ToolSummary, error)
	// Call invokes a tool with the given arguments.
	Call(ctx context.Context, name string, args map[string]any) (map[string]any, error)
}

// NewClientForConfig returns the appropriate Client for a given ServerConfig.
// For stdio servers, returns a stdioClientAdapter that wraps MCPProxy.
// For http/sse servers, returns an HTTPClient.
func NewClientForConfig(config types.ServerConfig) (Client, error) {
	switch config.Type {
	case "stdio":
		return &stdioClientAdapter{config: config}, nil
	case "http", "sse":
		url := config.Env["NEURALGENTICS_MCP_URL"]
		if url == "" {
			return nil, fmt.Errorf("http/sse server %q requires NEURALGENTICS_MCP_URL env var", config.Name)
		}
		authHeader := ""
		if v, ok := config.Env["NEURALGENTICS_MCP_AUTH"]; ok {
			authHeader = v
		}
		return NewHTTPClient(url, authHeader), nil
	default:
		return nil, fmt.Errorf("unknown server type: %q", config.Type)
	}
}

// stdioClientAdapter wraps the stdio MCPProxy lifecycle (Launch + Initialize + ListTools + Call)
// into the Client interface. This lets the broker use a single code path for both stdio and HTTP.
type stdioClientAdapter struct {
	config   types.ServerConfig
	proxy    *MCPProxy
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	launched bool
}

// Initialize launches the subprocess, starts the reader, and performs the MCP handshake.
func (s *stdioClientAdapter) Initialize(_ context.Context) error {
	cmd, stdin, stdout, err := launcher.BuildCommand(s.config)
	if err != nil {
		return fmt.Errorf("build stdio command for %q: %w", s.config.Name, err)
	}
	cmd.Env = buildEnv(s.config)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start subprocess for %q: %w", s.config.Name, err)
	}

	s.stdin = stdin
	s.stdout = stdout
	s.proxy = NewMCPProxy()
	s.proxy.StartReader(stdout)
	s.launched = true

	if err := s.proxy.Initialize(s.config.Name, stdin, stdout); err != nil {
		s.proxy.Stop()
		return fmt.Errorf("initialize %q: %w", s.config.Name, err)
	}
	return nil
}

// ListTools requests the tool list from the stdio server.
func (s *stdioClientAdapter) ListTools(_ context.Context) ([]types.ToolSummary, error) {
	if !s.launched {
		return nil, fmt.Errorf("stdio client not initialized")
	}
	return s.proxy.ListTools(s.config.Name, s.stdin, s.stdout)
}

// Call invokes a tool on the stdio server.
func (s *stdioClientAdapter) Call(_ context.Context, name string, args map[string]any) (map[string]any, error) {
	if !s.launched {
		return nil, fmt.Errorf("stdio client not initialized")
	}
	params := map[string]any{"name": name, "arguments": args}
	return s.proxy.Call(s.config.Name, "tools/call", params, s.stdin, s.stdout)
}

// buildEnv constructs the environment for a subprocess from config.Env plus
// the current process environment.
func buildEnv(config types.ServerConfig) []string {
	env := execEnv()
	for k, v := range config.Env {
		// Skip NEURALGENTICS_MCP_URL and NEURALGENTICS_MCP_AUTH which are
		// HTTP-only env vars — not relevant to stdio subprocesses.
		if k == "NEURALGENTICS_MCP_URL" || k == "NEURALGENTICS_MCP_AUTH" {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
}

// execEnv returns the current process environment as a slice.
func execEnv() []string {
	return os.Environ()
}
