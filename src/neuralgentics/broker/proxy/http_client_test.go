package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHTTPClient_Initialize verifies that Initialize stores the session ID
// from the Mcp-Session-Id response header.
func TestHTTPClient_Initialize(t *testing.T) {
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
