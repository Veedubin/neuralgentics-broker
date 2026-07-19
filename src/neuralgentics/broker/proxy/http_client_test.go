package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// TestHTTPClient_Initialize verifies that Initialize stores the session ID
// from the Mcp-Session-Id response header.
func TestHTTPClient_Initialize(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type.
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Set session ID in response header.
		w.Header().Set("Mcp-Session-Id", "test-session-123")

		// Read request body and extract id for response.
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo": map[string]any{
					"name":    "mock-http-server",
					"version": "1.0.0",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify session ID was stored.
	client.mu.Lock()
	sid := client.sessionID
	client.mu.Unlock()
	if sid != "test-session-123" {
		t.Errorf("expected session ID 'test-session-123', got %q", sid)
	}
}

// TestHTTPClient_ListTools verifies that ListTools parses tool summaries
// from the JSON-RPC response.
func TestHTTPClient_ListTools(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var resp map[string]any
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-listtools")
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
				},
			}
		case "tools/list":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []any{
						map[string]any{"name": "foo", "description": "foo tool"},
						map[string]any{"name": "bar", "description": "bar tool"},
					},
				},
			}
		default:
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")

	// Must initialize first to get session ID.
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "foo" || tools[0].Description != "foo tool" {
		t.Errorf("tool[0] = {%q, %q}, want {foo, foo tool}", tools[0].Name, tools[0].Description)
	}
	if tools[1].Name != "bar" || tools[1].Description != "bar tool" {
		t.Errorf("tool[1] = {%q, %q}, want {bar, bar tool}", tools[1].Name, tools[1].Description)
	}
}

// TestHTTPClient_Call verifies that Call sends a tools/call request and
// returns the result.
func TestHTTPClient_Call(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		method, _ := req["method"].(string)
		var resp map[string]any
		switch method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-call")
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{},
			}
		case "tools/call":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "hello from server"},
					},
				},
			}
		default:
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := client.Call(context.Background(), "greet", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	content, ok := result["content"]
	if !ok {
		t.Fatal("expected 'content' key in result")
	}
	_ = content // verified present
}

// TestHTTPClient_CallSSE verifies that CallSSE parses an SSE stream correctly.
func TestHTTPClient_CallSSE(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Accept header for SSE.
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept text/event-stream, got %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Mcp-Session-Id", "sess-sse")

		// Write SSE events.
		data := `{"jsonrpc":"2.0","id":4,"result":{"output":"sse-result"}}`
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")
	result, err := client.CallSSE(context.Background(), "test_tool", map[string]any{})
	if err != nil {
		t.Fatalf("CallSSE failed: %v", err)
	}
	if result["output"] != "sse-result" {
		t.Errorf("expected output 'sse-result', got %v", result["output"])
	}
}

// TestHTTPClient_AuthHeader verifies that the Authorization header is sent.
func TestHTTPClient_AuthHeader(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Mcp-Session-Id", "sess-auth")
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "Bearer secret-token-xyz")
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if receivedAuth != "Bearer secret-token-xyz" {
		t.Errorf("expected Authorization 'Bearer secret-token-xyz', got %q", receivedAuth)
	}
}

// TestHTTPClient_Error verifies that a server error (HTTP 500) is wrapped correctly.
func TestHTTPClient_Error(t *testing.T) {
	t.Setenv("EGRESS_GATEWAY_URL", "") // direct HTTP — no egress gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")
	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from Initialize, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected error containing 'HTTP 500', got %v", err)
	}
}

// TestProxyTransportUnset verifies that when EGRESS_GATEWAY_URL is unset
// (empty), proxyTransport returns http.DefaultTransport — preserving the
// pre-v0.13.0 direct-HTTP behavior of the broker.
func TestProxyTransportUnset(t *testing.T) {
	// Defensive: clear the env var in case the test runner has it set.
	t.Setenv("EGRESS_GATEWAY_URL", "")

	got := proxyTransport("")
	if got != http.DefaultTransport {
		t.Errorf("proxyTransport(\"\") = %p, want http.DefaultTransport (%p)", got, http.DefaultTransport)
	}

	// NewHTTPClient must also use the default transport when the env var is unset.
	client := NewHTTPClient("http://example.invalid", "")
	if client.httpClient.Transport != nil && client.httpClient.Transport != http.DefaultTransport {
		t.Errorf("NewHTTPClient transport = %v, want nil or http.DefaultTransport", client.httpClient.Transport)
	}
}

// TestProxyTransportSet verifies that when EGRESS_GATEWAY_URL is set to a
// valid URL, proxyTransport returns a proxy-aware *http.Transport whose
// Proxy function points at the gateway URL.
func TestProxyTransportSet(t *testing.T) {
	const gateway = "http://127.0.0.1:9090"

	rt := proxyTransport(gateway)
	transport, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("proxyTransport(%q) returned %T, want *http.Transport", gateway, rt)
	}
	if transport.Proxy == nil {
		t.Fatal("transport.Proxy is nil, expected http.ProxyURL helper")
	}
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.invalid"}}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy(req) error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != gateway {
		t.Errorf("transport.Proxy returned %v, want %q", proxyURL, gateway)
	}

	// Invalid gateway URL falls back to the default transport (fail-soft).
	rt = proxyTransport("://not-a-url")
	if rt != http.DefaultTransport {
		t.Errorf("proxyTransport(invalid) = %v, want http.DefaultTransport", rt)
	}
}

// TestEgressGatewayE2E is an end-to-end integration test for the transport
// swap. It spins up:
//   - a "gateway" httptest.Server that records every request it receives
//     (the audit log) and forwards them to the real MCP server,
//   - a "real" httptest.Server that mimics an MCP JSON-RPC endpoint.
//
// With EGRESS_GATEWAY_URL pointing at the gateway, the broker's HTTPClient
// should route its Initialize call through the gateway. The test verifies
// that (a) the JSON-RPC call still succeeds and (b) the gateway's audit log
// captured the proxied request.
func TestEgressGatewayE2E(t *testing.T) {
	// Real MCP server: responds to initialize with a session ID.
	realServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "e2e-session")
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "real-mcp", "version": "1.0.0"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer realServer.Close()

	// Gateway server: records requests and forwards to the real server.
	var (
		mu      sync.Mutex
		hits    int
		methods []string
	)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		methods = append(methods, r.Method)
		mu.Unlock()

		// Forward the request to the real MCP server.
		fwd, err := http.NewRequestWithContext(r.Context(), r.Method, realServer.URL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		fwd.Header = r.Header.Clone()
		resp, err := http.DefaultTransport.RoundTrip(fwd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer gateway.Close()

	// Point the broker at the egress gateway.
	t.Setenv("EGRESS_GATEWAY_URL", gateway.URL)

	client := NewHTTPClient(realServer.URL, "")
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize through gateway failed: %v", err)
	}

	// The gateway must have seen the request.
	mu.Lock()
	gotHits := hits
	gotMethods := methods
	mu.Unlock()
	if gotHits != 1 {
		t.Fatalf("expected 1 request through gateway, got %d", gotHits)
	}
	if len(gotMethods) != 1 || gotMethods[0] != http.MethodPost {
		t.Errorf("gateway methods = %v, want [POST]", gotMethods)
	}

	// Session ID must have propagated back through the gateway.
	client.mu.Lock()
	sid := client.sessionID
	client.mu.Unlock()
	if sid != "e2e-session" {
		t.Errorf("expected session ID 'e2e-session', got %q", sid)
	}
}
