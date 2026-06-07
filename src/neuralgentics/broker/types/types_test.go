package types

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTransportConfig_ToServerConfig_NPX(t *testing.T) {
	t.Parallel()
	tc := TransportConfig{
		Type:    TransportNPX,
		Package: "@modelcontextprotocol/server-github",
		Args:    []string{"--flag", "value"},
		Env:     map[string]string{"TOKEN": "abc"},
	}
	sc := tc.ToServerConfig("github-mcp", "GitHub MCP", []string{"filesystem"})
	if sc.Command != "npx" {
		t.Errorf("command: got %q, want %q", sc.Command, "npx")
	}
	if sc.Type != "stdio" {
		t.Errorf("type: got %q, want %q", sc.Type, "stdio")
	}
	// Args should be ["-y", "@modelcontextprotocol/server-github", "--flag", "value"]
	if len(sc.Args) != 4 {
		t.Fatalf("args length: got %d, want 4", len(sc.Args))
	}
	if sc.Args[0] != "-y" {
		t.Errorf("args[0]: got %q, want %q", sc.Args[0], "-y")
	}
	if sc.Args[1] != "@modelcontextprotocol/server-github" {
		t.Errorf("args[1]: got %q, want %q", sc.Args[1], "@modelcontextprotocol/server-github")
	}
	if sc.Args[2] != "--flag" {
		t.Errorf("args[2]: got %q, want %q", sc.Args[2], "--flag")
	}
	if sc.Args[3] != "value" {
		t.Errorf("args[3]: got %q, want %q", sc.Args[3], "value")
	}
	if sc.Env["TOKEN"] != "abc" {
		t.Errorf("env TOKEN: got %q, want %q", sc.Env["TOKEN"], "abc")
	}
	if sc.Name != "github-mcp" {
		t.Errorf("name: got %q, want %q", sc.Name, "github-mcp")
	}
	if sc.Description != "GitHub MCP" {
		t.Errorf("description: got %q, want %q", sc.Description, "GitHub MCP")
	}
}

func TestTransportConfig_ToServerConfig_UVX(t *testing.T) {
	t.Parallel()
	tc := TransportConfig{
		Type:    TransportUVX,
		Package: "mcp-server-filesystem",
	}
	sc := tc.ToServerConfig("fs-mcp", "", nil)
	if sc.Command != "uvx" {
		t.Errorf("command: got %q, want %q", sc.Command, "uvx")
	}
	if sc.Type != "stdio" {
		t.Errorf("type: got %q, want %q", sc.Type, "stdio")
	}
	if len(sc.Args) != 1 {
		t.Fatalf("args length: got %d, want 1", len(sc.Args))
	}
	if sc.Args[0] != "mcp-server-filesystem" {
		t.Errorf("args[0]: got %q, want %q", sc.Args[0], "mcp-server-filesystem")
	}
}

func TestTransportConfig_ToServerConfig_Local(t *testing.T) {
	t.Parallel()
	tc := TransportConfig{
		Type:    TransportLocal,
		Package: "/usr/local/bin/my-mcp-server",
		Args:    []string{"--port", "8080"},
	}
	sc := tc.ToServerConfig("local-mcp", "Local MCP", nil)
	if sc.Command != "/usr/local/bin/my-mcp-server" {
		t.Errorf("command: got %q, want %q", sc.Command, "/usr/local/bin/my-mcp-server")
	}
	if sc.Type != "stdio" {
		t.Errorf("type: got %q, want %q", sc.Type, "stdio")
	}
	if len(sc.Args) != 2 {
		t.Fatalf("args length: got %d, want 2", len(sc.Args))
	}
	if sc.Args[0] != "--port" || sc.Args[1] != "8080" {
		t.Errorf("args: got %v, want [--port 8080]", sc.Args)
	}
}

func TestTransportConfig_ToServerConfig_Docker(t *testing.T) {
	t.Parallel()
	tc := TransportConfig{
		Type:    TransportDocker,
		Package: "ghcr.io/example/mcp-server:v1",
		Args:    []string{"-e", "API_KEY=secret"},
	}
	sc := tc.ToServerConfig("docker-mcp", "Docker MCP", []string{"tools"})
	// Command should be docker or podman depending on what's installed
	if sc.Command != "docker" && sc.Command != "podman" {
		t.Errorf("command: got %q, want docker or podman", sc.Command)
	}
	if sc.Type != "stdio" {
		t.Errorf("type: got %q, want %q", sc.Type, "stdio")
	}
	// Args should start with ["run", "-i", "--rm", ...]
	if len(sc.Args) < 3 {
		t.Fatalf("args length: got %d, want at least 3", len(sc.Args))
	}
	if sc.Args[0] != "run" {
		t.Errorf("args[0]: got %q, want %q", sc.Args[0], "run")
	}
	if sc.Args[1] != "-i" {
		t.Errorf("args[1]: got %q, want %q", sc.Args[1], "-i")
	}
	if sc.Args[2] != "--rm" {
		t.Errorf("args[2]: got %q, want %q", sc.Args[2], "--rm")
	}
	// Find the image name in the args
	found := false
	for _, arg := range sc.Args {
		if arg == "ghcr.io/example/mcp-server:v1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("docker image not found in args: %v", sc.Args)
	}
}

func TestTransportConfig_ToServerConfig_HTTP(t *testing.T) {
	t.Parallel()
	tc := TransportConfig{
		Type: TransportHTTP,
		URL:  "https://mcp.example.com/api",
	}
	sc := tc.ToServerConfig("http-mcp", "HTTP MCP", nil)
	if sc.Command != "" {
		t.Errorf("command: got %q, want empty", sc.Command)
	}
	if sc.Type != "http" {
		t.Errorf("type: got %q, want %q", sc.Type, "http")
	}
	if sc.Env == nil {
		t.Fatal("env should not be nil for http transport with URL")
	}
	if sc.Env["NEURALGENTICS_MCP_URL"] != "https://mcp.example.com/api" {
		t.Errorf("env NEURALGENTICS_MCP_URL: got %q, want %q", sc.Env["NEURALGENTICS_MCP_URL"], "https://mcp.example.com/api")
	}
}

func TestMCPServerConfig_ToLegacyServerConfig_Empty(t *testing.T) {
	t.Parallel()
	cfg := MCPServerConfig{
		Name:       "empty-mcp",
		Transports: []TransportConfig{},
	}
	_, err := cfg.ToLegacyServerConfig()
	if err == nil {
		t.Fatal("expected error for empty transports, got nil")
	}
	if !strings.Contains(err.Error(), "has no transports") {
		t.Errorf("error: got %q, want substring %q", err.Error(), "has no transports")
	}
}

func TestMCPServerConfig_ToLegacyServerConfig_First(t *testing.T) {
	t.Parallel()
	cfg := MCPServerConfig{
		Name: "multi-mcp",
		Transports: []TransportConfig{
			{Type: TransportNPX, Package: "@org/mcp-server"},
			{Type: TransportDocker, Package: "ghcr.io/org/mcp-server:v1"},
		},
		Description:  "Multi transport",
		Capabilities: []string{"filesystem"},
	}
	sc, err := cfg.ToLegacyServerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use the first transport (npx)
	if sc.Command != "npx" {
		t.Errorf("command: got %q, want %q", sc.Command, "npx")
	}
	if sc.Name != "multi-mcp" {
		t.Errorf("name: got %q, want %q", sc.Name, "multi-mcp")
	}
	if sc.Description != "Multi transport" {
		t.Errorf("description: got %q, want %q", sc.Description, "Multi transport")
	}
	if len(sc.Capabilities) != 1 || sc.Capabilities[0] != "filesystem" {
		t.Errorf("capabilities: got %v, want [filesystem]", sc.Capabilities)
	}
}

func TestDetectContainerRuntime_PrefersDocker(t *testing.T) {
	t.Parallel()
	runtime := detectContainerRuntime()
	// On test machines, docker might or might not be installed.
	// Just verify it returns a valid string.
	if runtime != "docker" && runtime != "podman" {
		t.Errorf("detectContainerRuntime: got %q, want docker or podman", runtime)
	}
	// If docker is available, it should be preferred.
	dockerAvailable, _ := exec.LookPath("docker")
	if dockerAvailable != "" && runtime != "docker" {
		// Only check preference if docker is actually installed.
		// If podman is also installed, docker should still win.
		podmanAvailable, _ := exec.LookPath("podman")
		if podmanAvailable != "" {
			t.Errorf("detectContainerRuntime: docker is available but got %q, want docker", runtime)
		}
	}
}
