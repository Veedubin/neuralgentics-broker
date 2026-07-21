// package broker holds the README existence + content tests for T-131.
//
// These tests guard the standalone-install story for the
// neuralgentics-broker module: a new user running
// `go install github.com/Veedubin/neuralgentics-broker/cmd/broker@v<X.Y.Z>`
// (the pinned version parsed from CHANGELOG.md) should land on a
// README that tells them how to install, configure, audit, and route
// through the egress gateway. The tests are mechanical (substring
// presence + file size) so they survive copy edits.
//
// The pinned-version guard is evergreen: it parses the first
// `## [X.Y.Z]` heading from CHANGELOG.md and asserts the README pins
// that exact version. Future version bumps only touch CHANGELOG + README;
// the test never needs editing. Pinning (rather than `@latest`)
// gives users reproducible installs and avoids surprise breaking changes.
package broker

import (
	"os"
	"path/filepath"
	"regexp"
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

// changelogPath resolves packages/broker-go/CHANGELOG.md relative to
// this test file (same module-root computation as readmePath).
func changelogPath(t *testing.T) string {
	t.Helper()
	_, testFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", ".."))
	return filepath.Join(moduleRoot, "CHANGELOG.md")
}

// latestChangelogVersion reads CHANGELOG.md and returns the version
// string from the first `## [X.Y.Z]` heading. It fails the test if no
// such heading is found, since the broker must always have a current
// release entry.
func latestChangelogVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(changelogPath(t))
	if err != nil {
		t.Fatalf("CHANGELOG.md missing: %v", err)
	}
	return parseChangelogVersion(t, string(data))
}

// parseChangelogVersion extracts the first `## [X.Y.Z]` heading version
// from a CHANGELOG body. Exported via lowercase so the fixture test
// can drive it directly with synthetic input.
func parseChangelogVersion(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^##\s*\[(\d+\.\d+\.\d+)\]`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("CHANGELOG.md has no `## [X.Y.Z]` heading")
	}
	return m[1]
}

// pinnedInstallPath returns the canonical pinned `go install` target
// for a given semver, e.g. `github.com/Veedubin/neuralgentics-broker/cmd/broker@v0.1.3`.
func pinnedInstallPath(version string) string {
	return "github.com/Veedubin/neuralgentics-broker/cmd/broker@v" + version
}

// readmePinsVersion reports whether body mentions the pinned install
// path for the given version. Factored out so a fixture test can drive
// it with synthetic README content and prove the guard fails when the
// pin is absent.
func readmePinsVersion(body, version string) bool {
	return strings.Contains(body, pinnedInstallPath(version))
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
// documented AND pins the exact latest version parsed from CHANGELOG.md.
// This is the evergreen guard: a version bump that updates CHANGELOG +
// README but forgets to keep the pin in sync fails here. The `@latest`
// shortcut is intentionally NOT asserted — pinned installs are the
// reproducible default.
func TestReadmeMentionsInstallCommand(t *testing.T) {
	data, err := os.ReadFile(readmePath(t))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "go install") {
		t.Error("README.md does not mention `go install`")
	}
	version := latestChangelogVersion(t)
	if !readmePinsVersion(body, version) {
		t.Errorf("README.md does not pin the latest install path %q (CHANGELOG version %s)",
			pinnedInstallPath(version), version)
	}
}

// TestReadmePinnedVersionGuardFails proves the guard actually fails when
// the pinned version is absent — a regression net so a future edit that
// drops the pin (or swaps it back to `@latest`) is caught immediately
// rather than silently passing.
func TestReadmePinnedVersionGuardFails(t *testing.T) {
	version := "0.1.3"
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "pinned-version-present",
			body: "go install github.com/Veedubin/neuralgentics-broker/cmd/broker@v" + version,
			want: true,
		},
		{
			name: "latest-suffix-only",
			body: "go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest",
			want: false,
		},
		{
			name: "wrong-version-pinned",
			body: "go install github.com/Veedubin/neuralgentics-broker/cmd/broker@v0.0.1",
			want: false,
		},
		{
			name: "no-install-command",
			body: "Install via your package manager.",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readmePinsVersion(tc.body, version)
			if got != tc.want {
				t.Errorf("readmePinsVersion(%q, %q) = %v, want %v",
					tc.body, version, got, tc.want)
			}
		})
	}
}

// TestParseChangelogVersion proves the CHANGELOG parser picks the FIRST
// `## [X.Y.Z]` heading and ignores pre-release suffixes, unordered
// headings, and prose that mentions versions inline.
func TestParseChangelogVersion(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "simple-first-heading",
			body: "# Changelog\n\n## [0.1.3] - 2026-07-21\n\nText.\n\n## [0.1.2] - 2026-07-20\n",
			want: "0.1.3",
		},
		{
			name: "skips-non-version-headings",
			body: "# Changelog\n\n## Introduction\n\n## [1.2.0] - 2026-01-01\n",
			want: "1.2.0",
		},
		{
			name: "ignores-inline-version-prose",
			body: "# Changelog\n\nSome prose mentioning v9.9.9 inline.\n\n## [0.1.3] - 2026-07-21\n",
			want: "0.1.3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseChangelogVersion(t, tc.body)
			if got != tc.want {
				t.Errorf("parseChangelogVersion = %q, want %q", got, tc.want)
			}
		})
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
