package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// cronetServer stands in for GitHub's releases API: /releases lists them newest
// first, /releases/latest is the newest one, and /asset serves the payload. It
// counts asset downloads so a test can tell a fetch from a cache hit, and takes
// the digest verbatim so a test can serve a wrong or missing one.
type cronetServer struct {
	*httptest.Server
	downloads atomic.Int32
	lookups   atomic.Int32
}

// newCronetServerWith publishes the given releases, newest first.
func newCronetServerWith(t *testing.T, body []byte, digest string, tags []string, assetNames ...string) *cronetServer {
	t.Helper()
	cs := &cronetServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		cs.downloads.Add(1)
		_, _ = w.Write(body)
	})
	build := func(tag string) githubRelease {
		rel := githubRelease{TagName: tag}
		// Every release carries a library for a platform that is not ours, so a
		// test that finds the right asset has actually looked at the name.
		rel.Assets = append(rel.Assets, githubAsset{Name: "libcronet-linux-amd64.so", URL: cs.URL + "/asset", Digest: digest})
		for _, n := range assetNames {
			rel.Assets = append(rel.Assets, githubAsset{Name: n, URL: cs.URL + "/asset", Digest: digest})
		}
		return rel
	}
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		cs.lookups.Add(1)
		_ = json.NewEncoder(w).Encode(build(tags[0]))
	})
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		cs.lookups.Add(1)
		all := make([]githubRelease, 0, len(tags))
		for _, tag := range tags {
			all = append(all, build(tag))
		}
		_ = json.NewEncoder(w).Encode(all)
	})
	cs.Server = httptest.NewServer(mux)
	t.Cleanup(cs.Close)
	return cs
}

func newCronetServer(t *testing.T, tag, assetName string, body []byte, digest string) *cronetServer {
	t.Helper()
	return newCronetServerWith(t, body, digest, []string{tag}, assetName)
}

func (cs *cronetServer) api() string { return cs.URL + "/releases" }

func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// winPlan is what the manifest resolves to for a 64-bit Windows client. An
// empty version means "whatever upstream released last".
func winPlan(version string) cronetPlan {
	return cronetPlan{
		assetName:   "libcronet-windows-amd64.dll",
		installName: "libcronet.dll",
		version:     version,
	}
}

func TestCronetAssetName(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"windows", "amd64", "libcronet-windows-amd64.dll"},
		{"windows", "arm64", "libcronet-windows-arm64.dll"},
		// Upstream publishes no 386 dll, and darwin needs no library at all —
		// either way there is no asset to name, and the caller treats "" as
		// "nothing to install".
		{"windows", "386", ""},
		{"darwin", "arm64", ""},
		{"linux", "amd64", ""},
	} {
		if got := cronetAssetName(c.goos, c.goarch); got != c.want {
			t.Errorf("cronetAssetName(%s, %s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

// Upstream has tagged the same kind of release three different ways, so the
// version is matched as a whole component rather than by prefix.
func TestCronetTagMatches(t *testing.T) {
	const version = "150.0.7871.63"
	for _, c := range []struct {
		tag  string
		want bool
	}{
		{"v150.0.7871.63-1", true},       // the shape used today
		{"150.0.7871.63-2", true},        // an older release, tagged without the v
		{"v0.0.1-150.0.7871.63-1", true}, // older still, with a package version in front
		{"v150.0.7871.63", true},
		{"150.0.7871.63", true},
		// A prefix match would accept these, and they are different builds.
		{"v150.0.7871.630-1", false},
		{"v150.0.7871.6-1", false},
		{"v148.0.7778.96-1", false},
		{"", false},
	} {
		if got := cronetTagMatches(c.tag, version); got != c.want {
			t.Errorf("cronetTagMatches(%q, %q) = %v, want %v", c.tag, version, got, c.want)
		}
	}
	if cronetTagMatches("v150.0.7871.63-1", "") {
		t.Error("an empty version must not match a tag")
	}
}

// The point of the manifest block: the client installs the build the server
// named, not the newest one upstream happens to have published.
func TestEnsureCronet_InstallsThePinnedVersionNotTheLatest(t *testing.T) {
	body := []byte("PINNED-CRONET")
	srv := newCronetServerWith(t, body, sha256Digest(body),
		[]string{"v151.0.9999.1-1", "v150.0.7871.63-1", "v148.0.7778.96-1"},
		"libcronet-windows-amd64.dll")
	dataDir := t.TempDir()

	got, err := ensureCronet(srv.api(), dataDir, winPlan("150.0.7871.63"), nil)
	if err != nil {
		t.Fatalf("ensureCronet: %v", err)
	}
	if got != filepath.Join(dataDir, "libcronet.dll") {
		t.Errorf("installed path = %q", got)
	}
	if v := installedCronetVersion(dataDir, "libcronet.dll"); v != "v150.0.7871.63-1" {
		t.Errorf("version marker = %q, want the pinned release and not v151.0.9999.1-1", v)
	}
}

// A pinned version that upstream does not have is reported rather than being
// quietly rounded to something else.
func TestEnsureCronet_PinnedVersionMissingUpstream(t *testing.T) {
	body := []byte("CRONET")
	srv := newCronetServerWith(t, body, sha256Digest(body),
		[]string{"v150.0.7871.63-1"}, "libcronet-windows-amd64.dll")
	dataDir := t.TempDir()

	_, err := ensureCronet(srv.api(), dataDir, winPlan("149.0.0.0"), nil)
	if err == nil || !strings.Contains(err.Error(), "149.0.0.0") {
		t.Fatalf("error = %v, want it to name the version that is missing", err)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "libcronet.dll")); statErr == nil {
		t.Error("a library was installed for a version that does not exist")
	}
}

// Once the pinned build is on disk nothing is fetched again — including when
// upstream publishes something newer, which is the whole point of pinning.
func TestEnsureCronet_PinnedVersionIsNotChasedForward(t *testing.T) {
	body := []byte("PINNED-CRONET")
	srv := newCronetServerWith(t, body, sha256Digest(body),
		[]string{"v151.0.9999.1-1", "v150.0.7871.63-1"}, "libcronet-windows-amd64.dll")
	dataDir := t.TempDir()

	for i := 0; i < 2; i++ {
		if _, err := ensureCronet(srv.api(), dataDir, winPlan("150.0.7871.63"), nil); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if n := srv.downloads.Load(); n != 1 {
		t.Errorf("asset downloaded %d times, want 1", n)
	}
	if n := srv.lookups.Load(); n != 1 {
		t.Errorf("release API called %d times, want only the initial install", n)
	}
	if v := installedCronetVersion(dataDir, "libcronet.dll"); v != "v150.0.7871.63-1" {
		t.Errorf("version marker = %q", v)
	}
}

// Moving the pin replaces what is installed, in either direction — a rollback
// is as much a pin change as an upgrade.
func TestEnsureCronet_FollowsThePinBackwards(t *testing.T) {
	newer := []byte("NEWER-CRONET")
	dataDir := t.TempDir()
	srv := newCronetServerWith(t, newer, sha256Digest(newer),
		[]string{"v151.0.9999.1-1", "v150.0.7871.63-1"}, "libcronet-windows-amd64.dll")

	if _, err := ensureCronet(srv.api(), dataDir, winPlan("151.0.9999.1"), nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := ensureCronet(srv.api(), dataDir, winPlan("150.0.7871.63"), nil); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if v := installedCronetVersion(dataDir, "libcronet.dll"); v != "v150.0.7871.63-1" {
		t.Errorf("version marker = %q, want the release the pin moved to", v)
	}
}

// The library has to land under the name cronet-go's loader searches for, not
// under the per-arch name it is published as: sing-box looks for exactly
// libcronet.dll beside its own executable and will not find the other.
func TestEnsureCronet_InstallsUnderTheLoadersName(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	srv := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", body, sha256Digest(body))
	dataDir := t.TempDir()

	got, err := ensureCronet(srv.api(), dataDir, winPlan(""), nil)
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
	if v := installedCronetVersion(dataDir, "libcronet.dll"); v != "v150.0.7871.63-1" {
		t.Errorf("version marker = %q", v)
	}
	// A left-over .new would mean the swap did not complete.
	if _, err := os.Stat(want + ".new"); err == nil {
		t.Error("temporary file was left behind")
	}
}

func TestEnsureCronet_SkipsDownloadWhenCurrent(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	srv := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", body, sha256Digest(body))
	dataDir := t.TempDir()

	if _, err := ensureCronet(srv.api(), dataDir, winPlan(""), nil); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := ensureCronet(srv.api(), dataDir, winPlan(""), nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if n := srv.downloads.Load(); n != 1 {
		t.Errorf("asset downloaded %d times, want 1 (the second call should hit the version marker)", n)
	}
}

func TestEnsureCronet_ReplacesAnOlderRelease(t *testing.T) {
	old := []byte("OLD-CRONET")
	dataDir := t.TempDir()
	srv := newCronetServer(t, "v149.0.0.0-1", "libcronet-windows-amd64.dll", old, sha256Digest(old))
	if _, err := ensureCronet(srv.api(), dataDir, winPlan(""), nil); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	fresh := []byte("NEW-CRONET")
	newer := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", fresh, sha256Digest(fresh))
	if _, err := ensureCronet(newer.api(), dataDir, winPlan(""), nil); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	onDisk, _ := os.ReadFile(filepath.Join(dataDir, "libcronet.dll"))
	if string(onDisk) != string(fresh) {
		t.Errorf("library after upgrade = %q, want the new release", onDisk)
	}
	if v := installedCronetVersion(dataDir, "libcronet.dll"); v != "v150.0.7871.63-1" {
		t.Errorf("version marker after upgrade = %q", v)
	}
}

// The library is dlopen'd by the process that runs the tunnel, so a payload
// that does not match the checksum GitHub published must not reach the disk.
func TestEnsureCronet_RejectsUnverifiedPayload(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	for name, digest := range map[string]string{
		"digest mismatch":   sha256Digest([]byte("something else entirely")),
		"digest missing":    "",
		"digest not sha256": "md5:" + fmt.Sprintf("%x", sha256.Sum256(body)),
	} {
		t.Run(name, func(t *testing.T) {
			srv := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", body, digest)
			dataDir := t.TempDir()
			if _, err := ensureCronet(srv.api(), dataDir, winPlan(""), nil); err == nil {
				t.Fatal("expected an error")
			}
			if _, err := os.Stat(filepath.Join(dataDir, "libcronet.dll")); err == nil {
				t.Error("an unverified library was installed")
			}
		})
	}
}

// A release that GitHub cannot be asked about is only fatal when there is
// nothing on disk: an installed library keeps working while GitHub is down.
func TestEnsureCronet_UnreachableReleaseAPI(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unreachable.Close()

	dataDir := t.TempDir()
	if _, err := ensureCronet(unreachable.URL, dataDir, winPlan(""), nil); err == nil {
		t.Error("with no library installed the failure should be reported")
	}

	dest := filepath.Join(dataDir, "libcronet.dll")
	if err := os.WriteFile(dest, []byte("ALREADY-HERE"), 0o644); err != nil {
		t.Fatal(err)
	}
	var warned bool
	got, err := ensureCronet(unreachable.URL, dataDir, winPlan(""), func(_, level string) {
		if level == "WARN" {
			warned = true
		}
	})
	if err != nil || got != dest {
		t.Fatalf("with a library installed: %q, %v", got, err)
	}
	if !warned {
		t.Error("the skipped update check should be reported")
	}
	if onDisk, _ := os.ReadFile(dest); string(onDisk) != "ALREADY-HERE" {
		t.Errorf("installed library was disturbed: %q", onDisk)
	}
}

func TestEnsureCronet_ReleaseWithoutThisPlatformsAsset(t *testing.T) {
	body := []byte("FAKE-CRONET-DLL")
	// A release carrying only the amd64 dll, asked for arm64.
	srv := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", body, sha256Digest(body))
	dataDir := t.TempDir()
	plan := cronetPlan{assetName: "libcronet-windows-arm64.dll", installName: "libcronet.dll"}
	_, err := ensureCronet(srv.api(), dataDir, plan, nil)
	if err == nil || !strings.Contains(err.Error(), "libcronet-windows-arm64.dll") {
		t.Fatalf("error = %v, want it to name the missing asset", err)
	}
}

// Nothing is fetched, and no error raised, on a platform that needs no library.
func TestEnsureCronet_PlatformWithoutALibrary(t *testing.T) {
	srv := newCronetServer(t, "v150.0.7871.63-1", "libcronet-windows-amd64.dll", []byte("x"), sha256Digest([]byte("x")))
	dataDir := t.TempDir()
	got, err := ensureCronet(srv.api(), dataDir, cronetPlan{}, nil)
	if got != "" || err != nil {
		t.Fatalf("empty plan: %q, %v", got, err)
	}
	entries, _ := os.ReadDir(dataDir)
	if len(entries) != 0 {
		t.Errorf("wrote %d files into the sing-box directory", len(entries))
	}
	if n := srv.downloads.Load(); n != 0 {
		t.Errorf("downloaded the asset %d times", n)
	}
}
