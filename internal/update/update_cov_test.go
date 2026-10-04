package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The manifest's format decides the extractor; with none, the payload's magic
// bytes do.
func TestExtractSingboxPayloadPicksTheExtractor(t *testing.T) {
	exe := singboxFilenameForInstall()
	for _, tc := range []struct {
		name, format string
		body         []byte
	}{
		{"tar.gz named", "tar.gz", makeTarGz(t, "sing-box-1.0/"+exe, []byte("BIN"))},
		{"tgz named", " TGZ ", makeTarGz(t, "sing-box-1.0/"+exe, []byte("BIN"))},
		{"zip sniffed", "", makeZip(t, "release/"+exe, []byte("BIN"))},
		{"tar.gz sniffed", "", makeTarGz(t, "sing-box-1.0/"+exe, []byte("BIN"))},
		{"raw binary", "", []byte("BIN")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			got, err := extractSingboxPayload(tc.body, dir, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if got != filepath.Join(dir, exe) {
				t.Errorf("installed to %q", got)
			}
			if body, _ := os.ReadFile(got); string(body) != "BIN" {
				t.Errorf("installed binary = %q", body)
			}
		})
	}
}

// A zip entry compressed with a method Go cannot read is reported, not
// installed half-read.
func TestExtractZipUnsupportedCompression(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: singboxFilenameForInstall(), Method: 99})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("opaque")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, err = extractSingboxExeFromZip(buf.Bytes(), dir)
	if !errors.Is(err, zip.ErrAlgorithm) || !strings.Contains(err.Error(), "open zip entry") {
		t.Fatalf("err = %v, want an open zip entry error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files left behind: %v", entries)
	}
}

// Installing fails cleanly when the data directory is gone or the binary's
// place is taken by a directory, and no .new file is left behind.
func TestExtractInstallFailures(t *testing.T) {
	exe := singboxFilenameForInstall()
	for _, tc := range []struct {
		name    string
		extract func([]byte, string) (string, error)
		payload []byte
	}{
		{"zip", extractSingboxExeFromZip, makeZip(t, "release/"+exe, []byte("BIN"))},
		{"tar.gz", extractSingboxFromTarGz, makeTarGz(t, "sing-box-1.0/"+exe, []byte("BIN"))},
	} {
		t.Run(tc.name+" missing dir", func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "gone")
			if _, err := tc.extract(tc.payload, missing); err == nil {
				t.Fatal("extracting into a missing directory succeeded")
			}
		})
		t.Run(tc.name+" destination is a directory", func(t *testing.T) {
			dir := t.TempDir()
			blocker := filepath.Join(dir, exe)
			if err := os.MkdirAll(filepath.Join(blocker, "keep"), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := tc.extract(tc.payload, dir)
			if err == nil || !strings.Contains(err.Error(), "install exe") {
				t.Fatalf("err = %v, want an install error", err)
			}
			if _, err := os.Stat(blocker + ".new"); !os.IsNotExist(err) {
				t.Errorf(".new left behind: %v", err)
			}
			if fi, err := os.Stat(blocker); err != nil || !fi.IsDir() {
				t.Errorf("the directory in the way was touched: %v", err)
			}
		})
	}
}

// A library that downloads and verifies but cannot be recorded is an error:
// without the marker the next start could not tell what it has.
func TestEnsureCronetMarkerWriteFails(t *testing.T) {
	body := []byte("CRONET")
	srv := newLibraryServer(t, body)
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, cronetVersionFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ensureCronet(dataDir, winPlan(srv.libraryURL(), body), nil)
	if err == nil || !strings.HasPrefix(err.Error(), "cronet: write version marker") {
		t.Fatalf("err = %v, want the marker write to fail", err)
	}
}

// EnsureCronet takes the library from the manifest's block, and refuses a
// cleartext URL to a real host before any request is made.
func TestEnsureCronetFollowsTheManifest(t *testing.T) {
	InvalidateManifestCache()
	t.Cleanup(InvalidateManifestCache)
	setManifest := func(libraryURL string, body []byte) {
		manifestMu.Lock()
		cachedManifest = &SingboxManifest{Naive: &NaiveLibrary{
			Platforms: []string{runtime.GOOS + "-" + runtime.GOARCH},
			URL:       libraryURL,
			SHA256:    sha256Hex(body),
		}}
		cachedErr = nil
		cachedAt = time.Now()
		manifestMu.Unlock()
	}

	body := []byte("CRONET")
	dataDir := t.TempDir()
	setManifest("http://updates.example.invalid/libcronet-windows-amd64.dll", body)
	got, err := EnsureCronet(dataDir, nil)
	if err == nil || got != "" || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("EnsureCronet = %q, %v; want the cleartext URL refused", got, err)
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}

	srv := newLibraryServer(t, body)
	setManifest(srv.libraryURL(), body)
	got, err = EnsureCronet(dataDir, nil)
	if err != nil || got != filepath.Join(dataDir, CronetLibraryFileName) {
		t.Fatalf("EnsureCronet = %q, %v; want the library installed", got, err)
	}
}

// Without a manifest nothing says which library to fetch: an installed one
// keeps serving, and with none installed the manifest error is what is reported.
func TestEnsureCronetWithoutAManifest(t *testing.T) {
	manifestErr := errors.New("manifest HTTP 503")
	dataDir := t.TempDir()
	if _, err := ensureCronetFrom(nil, manifestErr, "windows", "amd64", dataDir, nil); err == nil || !strings.Contains(err.Error(), "manifest HTTP 503") {
		t.Fatalf("err = %v, want the manifest error", err)
	}
	dest := filepath.Join(dataDir, CronetLibraryFileName)
	if err := os.WriteFile(dest, []byte("ALREADY-HERE"), 0o644); err != nil {
		t.Fatal(err)
	}
	var warned bool
	got, err := ensureCronetFrom(nil, manifestErr, "windows", "amd64", dataDir, func(_, level string) {
		warned = warned || level == "WARN"
	})
	if err != nil || got != dest || !warned {
		t.Fatalf("with a library installed: %q, %v, warned %v", got, err, warned)
	}
	// macOS loads no library, so a missing manifest costs it nothing.
	if got, err := ensureCronetFrom(nil, manifestErr, "darwin", "arm64", t.TempDir(), nil); got != "" || err != nil {
		t.Fatalf("darwin: %q, %v", got, err)
	}
}
