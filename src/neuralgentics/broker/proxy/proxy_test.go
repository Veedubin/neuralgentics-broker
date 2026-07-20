package proxy

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker/types"
)

// mockServer simulates an MCP server by reading JSON-RPC requests from stdin
// and writing responses to stdout.
type mockServer struct {
	stdin  io.ReadCloser
	stdout io.WriteCloser
}

func newMockServer() (*mockServer, io.Writer, io.Reader) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	return &mockServer{stdin: stdinR, stdout: stdoutW}, stdinW, stdoutR
}

// run starts the mock server, reading requests and dispatching responses.
func (s *mockServer) run(t *testing.T) {
	t.Helper()
	defer s.stdin.Close()
	defer s.stdout.Close()

	decoder := json.NewDecoder(s.stdin)
	for decoder.More() {
		var req jsonrpcRequest
		if err := decoder.Decode(&req); err != nil {
			return
		}

		var resp jsonrpcResponse
		switch req.Method {
		case "initialize":
			resp = jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"0.1.0"}}`),
			}
		case "tools/list":
			resp = jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: json.RawMessage(`{
					"tools": [
						{"name": "read_file", "description": "Read a file from disk"},
						{"name": "write_file", "description": "Write content to a file on disk"}
					]
				}`),
			}
		case "tools/call":
			resp = jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"result"}]}`),
			}
		default:
			resp = jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonrpcError{Code: -32601, Message: "method not found"},
			}
		}

		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		s.stdout.Write(data)
	}
}

// closePipes safely closes both pipe ends of the mock server.
func (s *mockServer) closePipes() {
	s.stdin.Close()
	s.stdout.Close()
}

func TestNewMCPProxy(t *testing.T) {
	p := NewMCPProxy()
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.nextID != 1 {
		t.Fatalf("expected nextID=1, got %d", p.nextID)
	}
	if len(p.pending) != 0 {
		t.Fatalf("expected empty pending map, got %d entries", len(p.pending))
	}
}

func TestInitialize(t *testing.T) {
	srv, stdinW, stdoutR := newMockServer()
	go srv.run(t)
	defer srv.closePipes()

	p := NewMCPProxy()
	p.StartReader(stdoutR)
	defer p.Stop()

	err := p.Initialize("test-server", stdinW, stdoutR)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
}

func TestListTools(t *testing.T) {
	srv, stdinW, stdoutR := newMockServer()
	go srv.run(t)
	defer srv.closePipes()

	p := NewMCPProxy()
	p.StartReader(stdoutR)
	defer p.Stop()

	// Initialize first.
	if err := p.Initialize("test-server", stdinW, stdoutR); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools, err := p.ListTools("test-server", stdinW, stdoutR)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("expected tool[0].Name=read_file, got %s", tools[0].Name)
	}
	if tools[0].Server != "test-server" {
		t.Errorf("expected tool[0].Server=test-server, got %s", tools[0].Server)
	}
	if tools[1].Name != "write_file" {
		t.Errorf("expected tool[1].Name=write_file, got %s", tools[1].Name)
	}
}

func TestCall(t *testing.T) {
	srv, stdinW, stdoutR := newMockServer()
	go srv.run(t)
	defer srv.closePipes()

	p := NewMCPProxy()
	p.StartReader(stdoutR)
	defer p.Stop()

	if err := p.Initialize("test-server", stdinW, stdoutR); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := p.Call("test-server", "tools/call", map[string]any{
		"name": "read_file",
		"arguments": map[string]any{
			"path": "/tmp/test.txt",
		},
	}, stdinW, stdoutR)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	content, ok := result["content"]
	if !ok {
		t.Fatal("expected 'content' key in result")
	}
	_ = content // result exists
}

func TestStartReader_AsyncResponse(t *testing.T) {
	// Test that StartReader correctly routes responses by id to pending channels.
	p := NewMCPProxy()

	// Create a pipe to simulate server stdout.
	serverReader, serverWriter := io.Pipe()

	p.StartReader(serverReader)
	defer p.Stop()

	// Register a pending request manually.
	ch := make(chan *jsonrpcResponse, 1)
	p.mu.Lock()
	p.pending[42] = ch
	p.mu.Unlock()

	// Write a response from the "server" with id=42.
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      42,
		Result:  json.RawMessage(`{"status":"ok"}`),
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	serverWriter.Write(data)

	// The pending channel should receive the response.
	select {
	case got := <-ch:
		if got.ID != 42 {
			t.Errorf("expected id=42, got %d", got.ID)
		}
		if got.Error != nil {
			t.Errorf("expected no error, got %v", got.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response on pending channel")
	}

	serverWriter.Close()
}

func TestStartReader_Notification(t *testing.T) {
	// Test that notifications (no id field) are routed to the handler.
	p := NewMCPProxy()

	serverReader, serverWriter := io.Pipe()

	var notifMethod string
	var notifMu sync.Mutex
	p.SetNotificationHandler(func(method string, _ json.RawMessage) {
		notifMu.Lock()
		defer notifMu.Unlock()
		notifMethod = method
	})

	p.StartReader(serverReader)
	defer p.Stop()

	// Write a notification (no id field).
	notif := `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":50,"total":100}}` + "\n"
	serverWriter.Write([]byte(notif))

	// Wait for handler to fire.
	time.Sleep(100 * time.Millisecond)

	notifMu.Lock()
	if notifMethod != "notifications/progress" {
		t.Errorf("expected method=notifications/progress, got %s", notifMethod)
	}
	notifMu.Unlock()

	serverWriter.Close()
}

func TestStartReader_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent reader test in -short mode (slow, exercises mock subprocess lifecycle)")
	}
	// Test that multiple concurrent sendRPC calls receive their correct responses.
	srv, stdinW, stdoutR := newMockServer()
	go srv.run(t)
	defer srv.closePipes()

	p := NewMCPProxy()
	p.StartReader(stdoutR)
	defer p.Stop()

	if err := p.Initialize("test-server", stdinW, stdoutR); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Launch multiple ListTools calls concurrently.
	const numConcurrent = 5
	type result struct {
		tools []types.ToolSummary
		err   error
	}
	results := make(chan result, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func() {
			tools, err := p.ListTools("test-server", stdinW, stdoutR)
			results <- result{tools: tools, err: err}
		}()
	}

	for i := 0; i < numConcurrent; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent request %d failed: %v", i, r.err)
		}
		if len(r.tools) != 2 {
			t.Errorf("concurrent request %d: expected 2 tools, got %d", i, len(r.tools))
		}
	}
}

func TestStop_CleansUpPending(t *testing.T) {
	p := NewMCPProxy()

	// Don't use StartReader with a pipe — just test cleanup logic.
	// Register a pending channel that will be cleaned up on Stop.
	ch := make(chan *jsonrpcResponse, 1)
	p.mu.Lock()
	p.pending[1] = ch
	p.mu.Unlock()

	p.Stop()

	// After Stop, the channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected pending channel to be closed after Stop()")
	}

	// Pending map should be empty.
	p.mu.Lock()
	pendingCount := len(p.pending)
	p.mu.Unlock()
	if pendingCount != 0 {
		t.Errorf("expected 0 pending entries after Stop(), got %d", pendingCount)
	}
}

func TestSendRPC_NoReader(t *testing.T) {
	// sendRPC without StartReader should return an error.
	p := NewMCPProxy()
	output := &strings.Builder{}

	_, err := p.Call("test-server", "ping", map[string]any{}, output, nil)
	if err == nil {
		t.Fatal("expected error when calling sendRPC without StartReader")
	}
	if !strings.Contains(err.Error(), "StartReader") {
		t.Errorf("expected StartReader error, got: %v", err)
	}
}
