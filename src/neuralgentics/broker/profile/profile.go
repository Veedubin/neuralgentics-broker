// Package profile provides export/import of neuralgentics broker state
// to/from a portable tar.gz archive. Profiles capture the active LLM
// provider, active MCPs and their chosen transports, permission matrix
// snapshot, and a copy of opencode.json. Profiles can be signed with
// HMAC-SHA256 using a user-provided passphrase for integrity verification.
package profile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

const (
	CurrentVersion = "1.0.0"
	SignatureFile  = "signature.bin"
	ManifestFile   = "manifest.json"
)

// Profile is the in-memory representation of a profile archive.
type Profile struct {
	Manifest    Manifest        `json:"manifest"`
	Provider    json.RawMessage `json:"provider"`
	Catalog     json.RawMessage `json:"catalog"`
	Permissions json.RawMessage `json:"permissions"`
	Opencode    json.RawMessage `json:"opencode"`
	Pref        json.RawMessage `json:"pref"`
}

// Manifest is the metadata header of a profile archive.
type Manifest struct {
	Version       string `json:"version"`
	ExportedAt    string `json:"exported_at"`
	ExportedBy    string `json:"exported_by"` // hostname
	BrokerVersion string `json:"broker_version"`
	FileCount     int    `json:"file_count"`
}

// FileEntry describes a single file within the tar.gz archive.
type FileEntry struct {
	Name string
	Body []byte
}

// Build creates an in-memory Profile from raw JSON blobs.
func Build(providerJSON, catalogJSON, permissionsJSON, opencodeJSON, prefJSON []byte, brokerVersion string) *Profile {
	hostname, _ := os.Hostname()
	return &Profile{
		Manifest: Manifest{
			Version:       CurrentVersion,
			ExportedAt:    time.Now().UTC().Format(time.RFC3339),
			ExportedBy:    hostname,
			BrokerVersion: brokerVersion,
		},
		Provider:    providerJSON,
		Catalog:     catalogJSON,
		Permissions: permissionsJSON,
		Opencode:    opencodeJSON,
		Pref:        prefJSON,
	}
}

// Export writes the profile as a tar.gz to w. If passphrase is non-empty,
// adds an HMAC-SHA256 signature over the archive contents.
func (p *Profile) Export(w io.Writer, passphrase string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	files := []FileEntry{
		{Name: "profile.json", Body: mustMarshal(p)},
		{Name: "provider-pref.json", Body: p.Pref},
		{Name: "catalog.lock.json", Body: p.Catalog},
		{Name: "permissions.json", Body: p.Permissions},
		{Name: "opencode.snapshot.json", Body: p.Opencode},
		{Name: "provider.json", Body: p.Provider},
	}
	p.Manifest.FileCount = len(files) + 1 // +1 for manifest itself

	manifestBytes := mustMarshal(p.Manifest)
	files = append(files, FileEntry{Name: ManifestFile, Body: manifestBytes})

	// Compute hash over all file contents in sorted (deterministic) order
	// for signing/verification. Must match the order used in Import.
	var archiveBuf bytes.Buffer
	sortedFiles := make([]FileEntry, len(files))
	copy(sortedFiles, files)
	sort.Slice(sortedFiles, func(i, j int) bool { return sortedFiles[i].Name < sortedFiles[j].Name })
	for _, f := range sortedFiles {
		archiveBuf.Write(f.Body)
	}

	for _, f := range files {
		hdr := &tar.Header{
			Name: f.Name,
			Mode: 0644,
			Size: int64(len(f.Body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header for %s: %w", f.Name, err)
		}
		if _, err := tw.Write(f.Body); err != nil {
			return fmt.Errorf("write tar body for %s: %w", f.Name, err)
		}
	}

	if passphrase != "" {
		archiveHash := sha256.Sum256(archiveBuf.Bytes())
		mac := hmac.New(sha256.New, []byte(passphrase))
		mac.Write(archiveHash[:])
		sig := mac.Sum(nil)
		hdr := &tar.Header{Name: SignatureFile, Mode: 0644, Size: int64(len(sig))}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write signature header: %w", err)
		}
		if _, err := tw.Write(sig); err != nil {
			return fmt.Errorf("write signature: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}

// Import reads a tar.gz profile from r, verifies the signature if present,
// and returns the parsed Profile. If passphrase is required (signature file
// present) but empty/missing, returns an error.
func Import(r io.Reader, passphrase string) (*Profile, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	files := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		files[hdr.Name] = body
	}

	sig, hasSig := files[SignatureFile]
	if hasSig && passphrase == "" {
		return nil, fmt.Errorf("profile is signed but no passphrase provided")
	}

	if hasSig {
		// Recompute the archive hash over the non-signature files
		// using deterministic (sorted) filename order.
		var names []string
		for name := range files {
			if name != SignatureFile {
				names = append(names, name)
			}
		}
		sort.Strings(names)

		var archiveBuf bytes.Buffer
		for _, name := range names {
			archiveBuf.Write(files[name])
		}
		archiveHash := sha256.Sum256(archiveBuf.Bytes())
		mac := hmac.New(sha256.New, []byte(passphrase))
		mac.Write(archiveHash[:])
		if !hmac.Equal(mac.Sum(nil), sig) {
			return nil, fmt.Errorf("signature verification failed (wrong passphrase or corrupted file)")
		}
	}

	manifestBytes, ok := files[ManifestFile]
	if !ok {
		return nil, fmt.Errorf("profile missing manifest.json")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	return &Profile{
		Manifest:    manifest,
		Provider:    files["provider.json"],
		Catalog:     files["catalog.lock.json"],
		Permissions: files["permissions.json"],
		Opencode:    files["opencode.snapshot.json"],
		Pref:        files["provider-pref.json"],
	}, nil
}

func mustMarshal(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("marshal: %v", err))
	}
	return b
}
