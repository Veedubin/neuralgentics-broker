// package broker holds the import-path consistency test for T-130.
package broker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestImportPathConsistency scans every .go file under the broker-go
// module (rooted at this package's directory) and fails if any file
// still references the pre-T-130 bare module path
// `neuralgentics-broker/src/...`. After T-130 the canonical import
// path is `github.com/Veedubin/neuralgentics-broker/src/...`; any
// leftover bare-path import would break `go install` from outside the
// dev box and would also break consumers (e.g. packages/backend-go)
// that depend on the published module path.
//
// We deliberately allow the bare token `neuralgentics-broker` to
// appear in non-import contexts (e.g. the MCP `clientInfo.name` field
// in proxy/http_client.go and proxy/proxy.go) — those are
// protocol-visible display names, not Go import paths.
func TestImportPathConsistency(t *testing.T) {
	// Locate the module root by ascending from this test file until we
	// find go.mod. A fixed number of ".." levels is NOT robust: in the
	// neuralgentics monorepo the module lives at packages/broker-go
	// (4 levels up from this file), but in the standalone extracted
	// repository it lives at the repo root (3 levels up). Ascending to
	// go.mod works in both layouts.
	_, testFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Dir(testFile)
	for {
		if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(moduleRoot)
		if parent == moduleRoot {
			t.Fatalf("could not locate go.mod above %s", testFile)
		}
		moduleRoot = parent
	}

	oldPath := "neuralgentics-broker/src/neuralgentics/broker"
	newPrefix := "github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker"
	var offenders []string

	err := filepath.Walk(moduleRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == ".venv" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip this test file itself — it necessarily mentions the
		// old path as the constant it's scanning for.
		if filepath.Base(path) == "import_path_test.go" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		body := string(data)
		// The new canonical path contains the old path as a substring,
		// so we strip out every occurrence of the new prefix first; any
		// remaining hit of oldPath is a genuine bare-path import.
		stripped := strings.ReplaceAll(body, newPrefix, "")
		if strings.Contains(stripped, oldPath) {
			rel, relErr := filepath.Rel(moduleRoot, path)
			if relErr != nil {
				rel = path
			}
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf("found %d .go file(s) still referencing the old bare module path %q (must be github.com/Veedubin/neuralgentics-broker/src/...):\n  %s",
			len(offenders), oldPath, strings.Join(offenders, "\n  "))
	}
}
