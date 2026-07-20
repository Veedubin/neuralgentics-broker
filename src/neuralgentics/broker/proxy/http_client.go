package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// HTTPClient is an MCP client that speaks JSON-RPC over HTTP and SSE.
// Used for hosted MCP servers (TransportType == "http") where the MCP
// server runs remotely and exposes an HTTP/SSE endpoint.
type HTTPClient struct {
	baseURL    string
	authHeader string // value of Authorization header (e.g. "Bearer xxx"), empty if none
	httpClient *http.Client
	sessionID  string // MCP session ID returned by Initialize
	mu         sync.Mutex
}

// proxyTransport returns the http.RoundTripper the HTTPClient should use.
//
// When gatewayURL is empty (EGRESS_GATEWAY_URL unset), it returns
// http.DefaultTransport — preserving the pre-v0.13.0 behavior of the
// broker making direct HTTP calls to MCP servers.
//
// When gatewayURL is set to a valid URL (e.g. "http://localhost:9090"),
// it returns an http.Transport that routes outbound requests through
// that egress gateway via http.ProxyURL. Timeouts mirror the default
// transport's values so gateway-mode behavior is consistent with direct
// mode. An invalid gatewayURL falls back to http.DefaultTransport so a
// misconfiguration never hard-breaks broker calls.
func proxyTransport(gatewayURL string) http.RoundTripper {
	if gatewayURL == "" {
		return http.DefaultTransport
	}
	parsed, err := url.Parse(gatewayURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return http.DefaultTransport
	}
	return &http.Transport{
		Proxy: http.ProxyURL(parsed),
		// Timeouts mirror http.DefaultTransport / DefaultTransport cloning.
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewHTTPClient creates an HTTP client for the given MCP server URL.
// authHeader is the value of the Authorization header (e.g. "Bearer xxx"); pass "" for no auth.
//
// Outbound HTTP is routed through the egress gateway when the
// EGRESS_GATEWAY_URL environment variable is set to a non-empty URL.
// When the variable is unset, behavior is identical to pre-v0.13.0
// (direct HTTP via http.DefaultTransport).
func NewHTTPClient(baseURL string, authHeader string) *HTTPClient {
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authHeader: authHeader,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: proxyTransport(os.Getenv("EGRESS_GATEWAY_URL")),
		},
	}
}

// Initialize performs the MCP initialize handshake over HTTP POST.
// Stores the session ID returned in the response's Mcp-Session-Id header.
func (h *HTTPClient) Initialize(ctx context.Context) error {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "neuralgentics-broker",
				"version": "0.13.0",
			},
		},
	}
	resp, sessionID, err := h.doRequest(ctx, body)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	h.mu.Lock()
	h.sessionID = sessionID
	h.mu.Unlock()
	if errMsg, ok := resp["error"]; ok {
		return fmt.Errorf("initialize returned error: %v", errMsg)
	}
	return nil
}

// ListTools calls tools/list and returns the tool summaries.
func (h *HTTPClient) ListTools(ctx context.Context) ([]types.ToolSummary, error) {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	}
	resp, _, err := h.doRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	if errMsg, ok := resp["error"]; ok {
		return nil, fmt.Errorf("list tools returned error: %v", errMsg)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("list tools: missing result")
	}
	rawTools, ok := result["tools"].([]any)
	if !ok {
		return nil, nil // no tools
	}
	tools := make([]types.ToolSummary, 0, len(rawTools))
	for _, t := range rawTools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		desc, _ := tm["description"].(string)
		tools = append(tools, types.ToolSummary{
			Name:        name,
			Description: desc,
		})
	}
	return tools, nil
}

// Call invokes a tool with the given arguments.
func (h *HTTPClient) Call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	resp, _, err := h.doRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("call %q: %w", name, err)
	}
	if errMsg, ok := resp["error"]; ok {
		return nil, fmt.Errorf("call %q returned error: %v", name, errMsg)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("call %q: missing result", name)
	}
	return result, nil
}

// CallSSE invokes a tool and parses the SSE response stream.
// Used when the server responds with text/event-stream instead of a single JSON object.
// Returns the first complete JSON-RPC result from the stream.
func (h *HTTPClient) CallSSE(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := h.doRequestRaw(ctx, body, "text/event-stream")
	if err != nil {
		return nil, fmt.Errorf("call SSE %q: %w", name, err)
	}
	defer raw.Body.Close()

	// Parse SSE: each event is "data: <json>\n\n"
	scanner := bufio.NewScanner(raw.Body)
	var eventData strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// End of event
			if eventData.Len() > 0 {
				var resp map[string]any
				if err := json.Unmarshal([]byte(eventData.String()), &resp); err == nil {
					if result, ok := resp["result"].(map[string]any); ok {
						return result, nil
					}
					if errMsg, ok := resp["error"]; ok {
						return nil, fmt.Errorf("call SSE %q returned error: %v", name, errMsg)
					}
				}
			}
			eventData.Reset()
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			eventData.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE scan: %w", err)
	}
	return nil, fmt.Errorf("call SSE %q: no result in stream", name)
}

// doRequest sends a JSON-RPC request and returns the parsed response + session ID from headers.
func (h *HTTPClient) doRequest(ctx context.Context, body map[string]any) (map[string]any, string, error) {
	raw, err := h.doRequestRaw(ctx, body, "application/json")
	if err != nil {
		return nil, "", err
	}
	defer raw.Body.Close()
	if raw.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(raw.Body)
		return nil, "", fmt.Errorf("HTTP %d: %s", raw.StatusCode, string(b))
	}
	var resp map[string]any
	if err := json.NewDecoder(raw.Body).Decode(&resp); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}
	sessionID := raw.Header.Get("Mcp-Session-Id")
	return resp, sessionID, nil
}

// doRequestRaw builds and sends the HTTP request. Returns the raw response.
func (h *HTTPClient) doRequestRaw(ctx context.Context, body map[string]any, accept string) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", h.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	if h.authHeader != "" {
		req.Header.Set("Authorization", h.authHeader)
	}
	h.mu.Lock()
	sid := h.sessionID
	h.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	return h.httpClient.Do(req)
}
