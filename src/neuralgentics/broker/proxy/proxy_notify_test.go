package proxy

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"
)

// TestNotificationHandler_FiresOnNotification verifies that a registered
// NotificationHandler is invoked when a notification (JSON-RPC message
// without an id field) arrives on the reader stream.
func TestNotificationHandler_FiresOnNotification(t *testing.T) {
	p := NewMCPProxy()

	// Track handler invocations.
	var mu sync.Mutex
	receivedMethods := []string{}
	receivedParams := []json.RawMessage{}

	p.SetNotificationHandler(func(method string, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		receivedMethods = append(receivedMethods, method)
		receivedParams = append(receivedParams, params)
	})

	// Create a pipe to simulate server stdout.
	serverReader, serverWriter := io.Pipe()

	p.StartReader(serverReader)
	defer p.Stop()

	// Write a notification (no id field).
	notif := `{"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{}}` + "\n"
	if _, err := serverWriter.Write([]byte(notif)); err != nil {
		t.Fatalf("failed to write notification: %v", err)
	}

	// Write a second notification.
	notif2 := `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":75}}` + "\n"
	if _, err := serverWriter.Write([]byte(notif2)); err != nil {
		t.Fatalf("failed to write second notification: %v", err)
	}

	// Wait for handler to process both.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedMethods) != 2 {
		t.Fatalf("expected 2 notification methods, got %d", len(receivedMethods))
	}
	if receivedMethods[0] != "notifications/tools/list_changed" {
		t.Errorf("expected method 'notifications/tools/list_changed', got %q", receivedMethods[0])
	}
	if receivedMethods[1] != "notifications/progress" {
		t.Errorf("expected method 'notifications/progress', got %q", receivedMethods[1])
	}

	// Verify params were received.
	if len(receivedParams) != 2 {
		t.Fatalf("expected 2 param payloads, got %d", len(receivedParams))
	}

	serverWriter.Close()
}

// TestNotificationHandler_NilHandlerDoesNotPanic verifies that when no
// notification handler is set (nil onNotify), incoming notification
// messages are silently ignored without causing a panic or error.
func TestNotificationHandler_NilHandlerDoesNotPanic(t *testing.T) {
	p := NewMCPProxy()
	// Don't call SetNotificationHandler — onNotify remains nil.

	// Create a pipe to simulate server stdout.
	serverReader, serverWriter := io.Pipe()

	p.StartReader(serverReader)
	defer p.Stop()

	// Write a notification (no id field) — should not panic.
	notif := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n"
	if _, err := serverWriter.Write([]byte(notif)); err != nil {
		t.Fatalf("failed to write notification: %v", err)
	}

	// Give the reader goroutine time to process.
	time.Sleep(100 * time.Millisecond)

	// If we reach here without panic, the test passes.
	// Read loop should simply skip notifications when handler is nil.

	serverWriter.Close()
}
