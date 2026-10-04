package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// seedManifest installs a manifest into the package cache so GetManifest resolves
// without any network call. The asset for the host platform points at downloadURL.
func seedManifest(downloadURL, sha, format string) {
	key := runtime.GOOS + "-" + runtime.GOARCH
	m := &SingboxManifest{
		Version: "1.13.7",
		// Root url/sha256 are the fallback manifestPayload uses on platforms without an
		// arch-specific asset key (e.g. the linux CI runner); windows/darwin use Assets[key].
		URL:    downloadURL,
		SHA256: sha,
		Assets: map[string]SingboxAsset{key: {URL: downloadURL, SHA256: sha, Format: format}},
	}
	manifestMu.Lock()
	cachedManifest = m
	cachedErr = nil
	cachedAt = time.Now()
	manifestMu.Unlock()
}

// TestDownloadAndExtract_Integration exercises the real download + verify + extract
// + install flow against a local HTTP server serving a zipped sing-box binary.
func TestDownloadAndExtract_Integration(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	// Same archive layout as TestExtractSingboxExeFromZip ("release/" prefix); the extractor matches
	// the entry by filename suffix, so the directory prefix is arbitrary but kept consistent here.
	zipData := makeZip(t, "release/"+singboxFilenameForInstall(), []byte("FAKE-SINGBOX"))
	sum := sha256.Sum256(zipData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	seedManifest(srv.URL+"/sing-box.zip", hex.EncodeToString(sum[:]), "zip")

	dataDir := t.TempDir()
	var logged bool
	exe, err := DownloadAndExtract(dataDir, func(string, string) { logged = true })
	if err != nil {
		t.Fatalf("DownloadAndExtract: %v", err)
	}
	if exe != filepath.Join(dataDir, singboxFilenameForInstall()) {
		t.Errorf("installed path = %q", exe)
	}
	body, _ := os.ReadFile(exe)
	if string(body) != "FAKE-SINGBOX" {
		t.Errorf("installed binary content = %q", body)
	}
	if !logged {
		t.Error("expected progress logging")
	}
}

func TestDownloadAndExtract_SHAMismatch(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	zipData := makeZip(t, singboxFilenameForInstall(), []byte("X"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	seedManifest(srv.URL+"/sb.zip", "deadbeef", "zip") // wrong hash
	if _, err := DownloadAndExtract(t.TempDir(), nil); err == nil {
		t.Error("SHA mismatch should fail the download")
	}
}

func TestDownloadAndExtract_RejectsCleartextRemote(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	// A remote (non-loopback) http URL must be refused before any network call.
	seedManifest("http://example.com/sing-box.zip", "deadbeef", "zip")
	_, err := DownloadAndExtract(t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected an https-scheme rejection, got %v", err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"example.com", "10.0.0.1", ""} {
		if isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = true, want false", h)
		}
	}
}

func TestDownloadAndExtract_HTTPError(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	seedManifest(srv.URL+"/sb.zip", "", "zip")
	if _, err := DownloadAndExtract(t.TempDir(), nil); err == nil {
		t.Error("HTTP 500 should fail the download")
	}
}

func TestEnsureSingbox_AlreadyInstalled(t *testing.T) {
	dataDir := t.TempDir()
	exePath := filepath.Join(dataDir, singboxFilenameForInstall())
	if err := os.WriteFile(exePath, []byte("present"), 0o755); err != nil {
		t.Fatal(err)
	}
	// EnsureSingbox should return the existing binary without a download.
	got, err := EnsureSingbox(dataDir, nil)
	if err != nil || got != exePath {
		t.Fatalf("EnsureSingbox returned %q, %v", got, err)
	}
}

func TestEnsureSingbox_DownloadsWhenMissing(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	zipData := makeZip(t, singboxFilenameForInstall(), []byte("DL"))
	sum := sha256.Sum256(zipData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()
	seedManifest(srv.URL+"/sb.zip", hex.EncodeToString(sum[:]), "zip")

	dataDir := t.TempDir()
	got, err := EnsureSingbox(dataDir, nil)
	if err != nil {
		t.Fatalf("EnsureSingbox download path: %v", err)
	}
	if body, _ := os.ReadFile(got); string(body) != "DL" {
		t.Errorf("downloaded binary = %q", body)
	}
}
