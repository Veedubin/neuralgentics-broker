// package broker holds the README existence + content tests for T-131.
//
// These tests guard the standalone-install story for the
// neuralgentics-broker module: a new user running
// `go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest`
// should land on a README that tells them how to install, configure,
// audit, and route through the egress gateway. The tests are mechanical
// (substring presence + file size) so they survive copy edits.
package broker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readmePath resolves packages/broker-go/README.md relative to this
// test file, which lives at
// packages/broker-go/src/neuralgentics/broker/readme_test.go
// (module root = 3 levels up: broker -> neuralgentics -> src -> broker-go).
func readmePath(t *testing.T) string {
	t.Helper()
	_, testFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", ".."))
	return filepath.Join(moduleRoot, "README.md")
}

// TestReadmeExists asserts the README is present and non-trivial.
func TestReadmeExists(t *testing.T) {
	p := readmePath(t)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("README.md missing at %s: %v", p, err)
	}
	if len(data) < 1000 {
		t.Fatalf("README.md too small: %d bytes (expected > 1000)", len(data))
	}
}

// TestReadmeMentionsInstallCommand asserts the go install one-liner is
// documented so a new user can install the binary.
func TestReadmeMentionsInstallCommand(t *testing.T) {
	data, err := os.ReadFile(readmePath(t))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "go install") {
		t.Error("README.md does not mention `go install`")
	}
	if !strings.Contains(body, "github.com/Veedubin/neuralgentics-broker/cmd/broker@latest") {
		t.Error("README.md does not mention the canonical `go install ... @latest` path")
	}
}

// TestReadmeMentionsEgressGateway asserts the egress-gateway integration
// is documented, since it is one of the headline features of the broker.
func TestReadmeMentionsEgressGateway(t *testing.T) {
	data, err := os.ReadFile(readmePath(t))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "neuralgentics-gateway") {
		t.Error("README.md does not mention `neuralgentics-gateway`")
	}
	if !strings.Contains(body, "EGRESS_GATEWAY_URL") {
		t.Error("README.md does not mention the `EGRESS_GATEWAY_URL` env var")
	}
}

// TestReadmeMentionsAudit asserts the audit section is present, since
// the broker's headline capability is recording every tool call.
func TestReadmeMentionsAudit(t *testing.T) {
	data, err := os.ReadFile(readmePath(t))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	body := string(data)
	for _, want := range []string{"## Audit", "broker_audit_log", "--audit=jsonl+pg"} {
		if !strings.Contains(body, want) {
			t.Errorf("README.md missing audit marker %q", want)
		}
	}
}
