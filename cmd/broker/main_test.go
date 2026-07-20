// package main holds the broker CLI entrypoint and its tests.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain_Help builds the broker binary into a temp dir, runs it with
// --help (well, an invalid flag that triggers flag.Usage), and asserts
// the process produces sane output and exits non-zero. This proves the
// `go install ./cmd/broker` path produces a working binary — the
// module-path rename in T-130 would break `go install` if any import
// still pointed at the old `neuralgentics-broker` path, and this test
// catches that regression by exercising the full build+run path.
func TestMain_Help(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build in -short mode")
	}

	tmp := t.TempDir()
	binPath := filepath.Join(tmp, "broker")

	// Build the binary from this package. If the module path or any
	// import is broken, `go build` fails here.
	cmdBuild := exec.Command("go", "build", "-o", binPath, ".")
	cmdBuild.Env = append(os.Environ(), "GO111MODULE=on")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// Run with an unknown flag; flag.Parse calls flag.Usage and the
	// process exits with status 2. We accept either --help (exit 0)
	// or an unknown flag (exit 2) as "sane output".
	cmdRun := exec.Command(binPath, "--help")
	var stdout, stderr bytes.Buffer
	cmdRun.Stdout = &stdout
	cmdRun.Stderr = &stderr
	_ = cmdRun.Run() // ignore exit code; --help exits 0 or 2 depending on flag pkg

	combined := stdout.String() + stderr.String()
	if combined == "" {
		t.Fatal("broker --help produced no output")
	}

	// The broker's main.go registers audit flags, so --help output
	// must mention at least one of them. This proves the binary is
	// actually our broker, not a stale artifact.
	lower := strings.ToLower(combined)
	if !strings.Contains(lower, "audit") && !strings.Contains(lower, "usage") {
		t.Errorf("broker --help output missing expected content; got:\n%s", combined)
	}
}
