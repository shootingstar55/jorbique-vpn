package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// libraryServer stands in for the update server's channel: it serves one
// library file and counts the downloads, so a test can tell a fetch from a
// cache hit.
type libraryServer struct {
	*httptest.Server
	downloads atomic.Int32
}

func newLibraryServer(t *testing.T, body []byte) *libraryServer {
	t.Helper()
	ls := &libraryServer{}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/libcronet-windows-amd64.dll" {
			http.NotFound(w, r)
			return
		}
		ls.downloads.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ls.Close)
	return ls
}

func (ls *libraryServer) libraryURL() string { return ls.URL + "/libcronet-windows-amd64.dll" }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// winPlan is what the manifest resolves to for a 64-bit Windows client.
func winPlan(libraryURL string, body []byte) cronetPlan {
	return cronetPlan{
		url:         libraryURL,
		sha256:      sha256Hex(body),
		installName: "libcronet.dll",
		version:     "154.0.8037.49",
	}
}

// The library has to land under the name cronet-go's loader searches for, not
// under the name the server publishes it as: sing-box looks for exactly
// libcronet.dll beside its own executable and will not find the other.
func TestEnsureCronet_InstallsUnderTheLoadersName(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	srv := newLibraryServer(t, body)
	dataDir := t.TempDir()

	got, err := ensureCronet(dataDir, winPlan(srv.libraryURL(), body), nil)
	if err != nil {
		t.Fatalf("ensureCronet: %v", err)
	}
	want := filepath.Join(dataDir, "libcronet.dll")
	if got != want {
		t.Errorf("installed path = %q, want %q", got, want)
	}
	onDisk, err := os.ReadFile(want)
	if err != nil || string(onDisk) != string(body) {
		t.Fatalf("library on disk = %q, %v", onDisk, err)
	}
	if v := installedCronetSHA256(dataDir, "libcronet.dll"); v != sha256Hex(body) {
		t.Errorf("marker = %q, want the library's sha256", v)
	}
	// A left-over .new would mean the swap did not complete.
	if _, err := os.Stat(want + ".new"); err == nil {
		t.Error("temporary file was left behind")
	}
}

// Once the named library is on disk nothing is fetched again.
func TestEnsureCronet_SkipsDownloadWhenCurrent(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	srv := newLibraryServer(t, body)
	dataDir := t.TempDir()

	for i := 0; i < 2; i++ {
		if _, err := ensureCronet(dataDir, winPlan(srv.libraryURL(), body), nil); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if n := srv.downloads.Load(); n != 1 {
		t.Errorf("library downloaded %d times, want 1 (the second call should hit the marker)", n)
	}
}

// A new sing-box on the server brings a new library, and the checksum is what
// says so: the installed one is replaced, whichever way the version moved.
func TestEnsureCronet_ReplacesALibraryTheServerMovedOn(t *testing.T) {
	dataDir := t.TempDir()
	old := []byte("OLD-CRONET")
	if _, err := ensureCronet(dataDir, winPlan(newLibraryServer(t, old).libraryURL(), old), nil); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	fresh := []byte("NEW-CRONET")
	if _, err := ensureCronet(dataDir, winPlan(newLibraryServer(t, fresh).libraryURL(), fresh), nil); err != nil {
		t.Fatalf("replace: %v", err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dataDir, "libcronet.dll"))
	if string(onDisk) != string(fresh) {
		t.Errorf("library after the server moved on = %q, want the new one", onDisk)
	}
	if v := installedCronetSHA256(dataDir, "libcronet.dll"); v != sha256Hex(fresh) {
		t.Errorf("marker after replace = %q", v)
	}
}

// The library is dlopen'd by the process that runs the tunnel, so a payload
// that does not match the checksum the manifest states must not reach the disk.
func TestEnsureCronet_RejectsUnverifiedPayload(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	srv := newLibraryServer(t, body)
	dataDir := t.TempDir()

	plan := winPlan(srv.libraryURL(), []byte("something else entirely"))
	if _, err := ensureCronet(dataDir, plan, nil); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("error = %v, want a checksum mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libcronet.dll")); err == nil {
		t.Error("an unverified library was installed")
	}
}

// Cleartext is refused for any real host, as for the sing-box download.
func TestEnsureCronet_RefusesCleartextToARealHost(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	dataDir := t.TempDir()
	plan := winPlan("http://updates.example.invalid/libcronet-windows-amd64.dll", body)
	if _, err := ensureCronet(dataDir, plan, nil); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("error = %v, want the cleartext URL refused", err)
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

// A server that cannot be reached is only fatal when there is nothing on disk:
// an installed library keeps working while the server is down.
func TestEnsureCronet_UnreachableServer(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unreachable.Close()
	body := []byte("NEW-CRONET")
	plan := winPlan(unreachable.URL+"/libcronet-windows-amd64.dll", body)

	dataDir := t.TempDir()
	if _, err := ensureCronet(dataDir, plan, nil); err == nil {
		t.Error("with no library installed the failure should be reported")
	}

	dest := filepath.Join(dataDir, "libcronet.dll")
	if err := os.WriteFile(dest, []byte("ALREADY-HERE"), 0o644); err != nil {
		t.Fatal(err)
	}
	var warned bool
	got, err := ensureCronet(dataDir, plan, func(_, level string) {
		if level == "WARN" {
			warned = true
		}
	})
	if err != nil || got != dest {
		t.Fatalf("with a library installed: %q, %v", got, err)
	}
	if !warned {
		t.Error("the skipped update should be reported")
	}
	if onDisk, _ := os.ReadFile(dest); string(onDisk) != "ALREADY-HERE" {
		t.Errorf("installed library was disturbed: %q", onDisk)
	}
}

// A manifest that names no library leaves nothing to fetch. That is reported —
// not guessed around — unless a library is already installed.
func TestEnsureCronet_ManifestNamesNoLibrary(t *testing.T) {
	dataDir := t.TempDir()
	plan := cronetPlan{installName: "libcronet.dll"}
	if _, err := ensureCronet(dataDir, plan, nil); err == nil || !strings.Contains(err.Error(), "names no library") {
		t.Fatalf("error = %v, want the missing library reported", err)
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}

	dest := filepath.Join(dataDir, "libcronet.dll")
	if err := os.WriteFile(dest, []byte("ALREADY-HERE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ensureCronet(dataDir, plan, nil); err != nil || got != dest {
		t.Fatalf("with a library installed: %q, %v", got, err)
	}
}
