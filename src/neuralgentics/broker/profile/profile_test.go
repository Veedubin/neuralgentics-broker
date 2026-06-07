package profile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"
)

func sampleJSON(key, val string) []byte {
	b, _ := json.Marshal(map[string]string{key: val})
	return b
}

func TestBuild_Profile(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test-catalog"),
		sampleJSON("permissions", "test-perms"),
		sampleJSON("opencode", "test-oc"),
		sampleJSON("pref", "test-pref"),
		"0.5.0",
	)
	if p.Manifest.Version != CurrentVersion {
		t.Errorf("expected version %s, got %s", CurrentVersion, p.Manifest.Version)
	}
	if p.Manifest.BrokerVersion != "0.5.0" {
		t.Errorf("expected broker_version 0.5.0, got %s", p.Manifest.BrokerVersion)
	}
	if p.Manifest.ExportedBy == "" {
		t.Error("expected exported_by hostname to be set")
	}
	if len(p.Provider) == 0 {
		t.Error("expected provider JSON to be set")
	}
	if len(p.Catalog) == 0 {
		t.Error("expected catalog JSON to be set")
	}
}

func TestExport_NoSignature(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, ""); err != nil {
		t.Fatalf("Export: %v", err)
	}

	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	fileNames := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		fileNames[hdr.Name] = true
		_, _ = io.ReadAll(tr)
	}

	expected := []string{
		"profile.json",
		"provider-pref.json",
		"catalog.lock.json",
		"permissions.json",
		"opencode.snapshot.json",
		"provider.json",
		"manifest.json",
	}
	for _, name := range expected {
		if !fileNames[name] {
			t.Errorf("expected file %s in archive, not found", name)
		}
	}
	if fileNames[SignatureFile] {
		t.Error("signature.bin should NOT be present when passphrase is empty")
	}
}

func TestExport_WithSignature(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, "secret"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == SignatureFile {
			found = true
			if hdr.Size != 32 {
				t.Errorf("expected signature size 32, got %d", hdr.Size)
			}
		}
		_, _ = io.ReadAll(tr)
	}
	if !found {
		t.Error("signature.bin not found in signed archive")
	}
}

func TestImport_RoundTrip(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "my-catalog"),
		sampleJSON("permissions", "my-perms"),
		sampleJSON("opencode", "my-oc"),
		sampleJSON("pref", "my-pref"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, ""); err != nil {
		t.Fatalf("Export: %v", err)
	}

	imported, err := Import(&buf, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if imported.Manifest.Version != CurrentVersion {
		t.Errorf("expected version %s, got %s", CurrentVersion, imported.Manifest.Version)
	}
	if imported.Manifest.BrokerVersion != "0.5.0" {
		t.Errorf("expected broker_version 0.5.0, got %s", imported.Manifest.BrokerVersion)
	}

	var provider map[string]string
	if err := json.Unmarshal(imported.Provider, &provider); err != nil {
		t.Fatalf("unmarshal provider: %v", err)
	}
	if provider["provider"] != "ollama-cloud" {
		t.Errorf("expected provider=ollama-cloud, got %v", provider)
	}
}

func TestImport_SignatureVerify(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, "mypassword"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	imported, err := Import(&buf, "mypassword")
	if err != nil {
		t.Fatalf("Import with correct passphrase: %v", err)
	}
	if imported.Manifest.Version != CurrentVersion {
		t.Errorf("expected version %s, got %s", CurrentVersion, imported.Manifest.Version)
	}
}

func TestImport_WrongPassphrase(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, "secret"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	_, err := Import(&buf, "wrong")
	if err == nil {
		t.Error("expected error for wrong passphrase, got nil")
	}
	if err.Error() != "signature verification failed (wrong passphrase or corrupted file)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestImport_MissingPassphrase(t *testing.T) {
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "test"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, "secret"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	_, err := Import(&buf, "")
	if err == nil {
		t.Error("expected error for missing passphrase, got nil")
	}
	if err.Error() != "profile is signed but no passphrase provided" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestImport_CorruptedFile(t *testing.T) {
	// Export a profile with a passphrase, then modify one of the content
	// files without updating the signature. The import should fail.
	p := Build(
		sampleJSON("provider", "ollama-cloud"),
		sampleJSON("catalog", "original-catalog"),
		sampleJSON("permissions", "test"),
		sampleJSON("opencode", "test"),
		sampleJSON("pref", "test"),
		"0.5.0",
	)
	var buf bytes.Buffer
	if err := p.Export(&buf, "secret"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Decompress, modify catalog.lock.json, recompress with original signature.
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(gz)

	var files []FileEntry
	var origSig []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		if hdr.Name == SignatureFile {
			origSig = body
			continue // Hold back the signature
		}
		// Tamper with catalog.lock.json
		if hdr.Name == "catalog.lock.json" {
			body = []byte(`{"tampered": true}`)
		}
		files = append(files, FileEntry{Name: hdr.Name, Body: body})
	}
	gz.Close()

	// Rebuild the archive with the tampered content but the original signature.
	var tamperBuf bytes.Buffer
	tw := tar.NewWriter(&tamperBuf)
	for _, f := range files {
		hdr := &tar.Header{Name: f.Name, Mode: 0644, Size: int64(len(f.Body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write(f.Body); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	// Append the original signature (which was computed over un-tampered content).
	sigHdr := &tar.Header{Name: SignatureFile, Mode: 0644, Size: int64(len(origSig))}
	if err := tw.WriteHeader(sigHdr); err != nil {
		t.Fatalf("write sig header: %v", err)
	}
	if _, err := tw.Write(origSig); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	tw.Close()

	// Now try to import the tampered archive.
	// But wait — our Import function computes the HMAC over file bodies,
	// not over the raw tar byte stream. The tampering changed catalog.lock.json
	// content, so the recomputed hash will differ from the original.
	// However, we wrote the tampered files AND the original sig into a raw
	// tar (not gzip). We need to wrap this in gzip.
	var finalBuf bytes.Buffer
	gzw := gzip.NewWriter(&finalBuf)
	// Copy the tar data into gzip
	gzw.Write(tamperBuf.Bytes())
	gzw.Close()

	// Actually, the Import function reads gzip, so we need to write
	// the tar entries directly into a gzip writer.
	// Let's redo: write tar into gzip properly.
	finalBuf.Reset()
	gzw = gzip.NewWriter(&finalBuf)
	tw2 := tar.NewWriter(gzw)
	for _, f := range files {
		hdr := &tar.Header{Name: f.Name, Mode: 0644, Size: int64(len(f.Body))}
		if err := tw2.WriteHeader(hdr); err != nil {
			t.Fatalf("write header 2: %v", err)
		}
		if _, err := tw2.Write(f.Body); err != nil {
			t.Fatalf("write body 2: %v", err)
		}
	}
	sigHdr2 := &tar.Header{Name: SignatureFile, Mode: 0644, Size: int64(len(origSig))}
	if err := tw2.WriteHeader(sigHdr2); err != nil {
		t.Fatalf("write sig header 2: %v", err)
	}
	if _, err := tw2.Write(origSig); err != nil {
		t.Fatalf("write sig 2: %v", err)
	}
	tw2.Close()
	gzw.Close()

	_, err = Import(&finalBuf, "secret")
	if err == nil {
		t.Error("expected error for corrupted/tampered file, got nil")
	}
	// The error should be about signature verification failure.
	if err != nil && err.Error() != "signature verification failed (wrong passphrase or corrupted file)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestImport_MissingManifest(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	body := []byte(`{"hello": "world"}`)
	hdr := &tar.Header{Name: "profile.json", Mode: 0644, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}

	tw.Close()
	gz.Close()

	_, err := Import(&buf, "")
	if err == nil {
		t.Error("expected error for missing manifest, got nil")
	}
	if err.Error() != "profile missing manifest.json" {
		t.Errorf("unexpected error message: %v", err)
	}
}
