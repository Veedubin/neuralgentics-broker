package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"os/exec"
)

//go:embed mcp_catalog.json
var curatedCatalogFS embed.FS

// curatedCatalogJSON holds the embedded catalog JSON bytes.
// This variable is populated at compile time by the //go:embed directive above.
var curatedCatalogJSON []byte

func init() {
	data, err := curatedCatalogFS.ReadFile("mcp_catalog.json")
	if err != nil {
		// This should never happen since the file is embedded and always present.
		panic(fmt.Sprintf("catalog: failed to read embedded mcp_catalog.json: %v", err))
	}
	curatedCatalogJSON = data
}

// CuratedCatalog is the in-memory representation of mcp_catalog.json.
type CuratedCatalog struct {
	Version     string          `json:"version"`
	Updated     string          `json:"updated"`
	Description string          `json:"description"`
	Servers     []CuratedServer `json:"servers"`
	Provenance  string          `json:"_provenance,omitempty"`
}

// CuratedServer describes a single curated MCP server entry.
type CuratedServer struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Homepage     string             `json:"homepage,omitempty"`
	Category     string             `json:"category"`
	Capabilities []string           `json:"capabilities"`
	Transports   []CuratedTransport `json:"transports"`
	RequiredEnv  []string           `json:"required_env,omitempty"`
}

// CuratedTransport describes a single transport option for a curated MCP server.
type CuratedTransport struct {
	Type        string            `json:"type"`                  // npx/uvx/local/docker/http
	Package     string            `json:"package,omitempty"`     // Package name or image name
	Args        []string          `json:"args,omitempty"`        // Extra args after the package name
	Env         map[string]string `json:"env,omitempty"`         // Environment variables
	URL         string            `json:"url,omitempty"`         // For http transport
	Default     bool              `json:"default,omitempty"`     // If true, preferred transport
	Description string            `json:"description,omitempty"` // Human-readable explanation
}

// LoadCuratedCatalog returns the embedded curated catalog.
// It unmarshals the embedded JSON each time it is called,
// so callers always get the current data.
func LoadCuratedCatalog() (*CuratedCatalog, error) {
	var cat CuratedCatalog
	if err := json.Unmarshal(curatedCatalogJSON, &cat); err != nil {
		return nil, fmt.Errorf("unmarshal curated catalog: %w", err)
	}
	return &cat, nil
}

// CheckTransportAvailability checks which transport types are available on the
// current system by verifying that the required binaries (npx, uvx, docker, podman)
// are on PATH. It returns two lists: available transports and unavailable transport
// type names.
func CheckTransportAvailability(transports []CuratedTransport) (available []CuratedTransport, unavailable []string) {
	// Pre-check which transport binaries exist.
	binaryAvailable := map[string]bool{
		"npx":    execLookPath("npx"),
		"uvx":    execLookPath("uvx"),
		"docker": execLookPath("docker"),
		"podman": execLookPath("podman"),
	}

	seenUnavailable := make(map[string]bool)
	for _, t := range transports {
		requiredBinary := transportBinary(t.Type)
		if requiredBinary == "" {
			// http/local transports don't require a specific binary on PATH.
			available = append(available, t)
			continue
		}
		if binaryAvailable[requiredBinary] {
			available = append(available, t)
		} else {
			if !seenUnavailable[t.Type] {
				unavailable = append(unavailable, t.Type)
				seenUnavailable[t.Type] = true
			}
		}
	}
	return available, unavailable
}

// transportBinary maps a transport type to the binary it needs on PATH.
// Returns empty string for transports that don't need a binary check.
func transportBinary(transportType string) string {
	switch transportType {
	case "npx":
		return "npx"
	case "uvx":
		return "uvx"
	case "docker":
		// docker transport can use either docker or podman.
		// The binary check is done separately for both.
		return "docker"
	case "local":
		return "" // No PATH check needed; the binary path is in Package.
	case "http":
		return "" // HTTP transport uses the built-in client, no binary needed.
	default:
		return ""
	}
}

// execLookPath wraps exec.LookPath for testability.
var execLookPath = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
