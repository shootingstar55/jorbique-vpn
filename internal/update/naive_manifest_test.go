package update

import (
	"encoding/json"
	"strings"
	"testing"
)

// manifestWithNaive is the block as the update server publishes it.
const manifestWithNaive = `{
  "version": "v1.13.19",
  "assets": {"windows-amd64": {"url": "https://example.invalid/sing-box.zip", "sha256": "aa", "format": "zip"}},
  "naive": {
    "platforms": ["windows-amd64"],
    "library_filename": "libcronet.dll",
    "cronet_version": "150.0.7871.63",
    "source_repo": "https://github.com/sagernet/cronet-go",
    "source_asset": "libcronet-windows-amd64.dll"
  }
}`

func parseManifest(t *testing.T, body string) *SingboxManifest {
	t.Helper()
	var m SingboxManifest
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return &m
}

func TestManifestNaiveBlockIsRead(t *testing.T) {
	m := parseManifest(t, manifestWithNaive)
	if m.Naive == nil {
		t.Fatal("the naive block was not parsed")
	}
	plan, ok := cronetPlanFor(m, "windows", "amd64")
	if !ok {
		t.Fatal("windows-amd64 is listed and should get a plan")
	}
	if plan.version != "150.0.7871.63" {
		t.Errorf("version = %q, want the pinned one", plan.version)
	}
	if plan.assetName != "libcronet-windows-amd64.dll" {
		t.Errorf("asset = %q", plan.assetName)
	}
	if plan.installName != "libcronet.dll" {
		t.Errorf("install name = %q", plan.installName)
	}
	api, err := cronetReleasesAPI(plan.repo)
	if err != nil {
		t.Fatalf("releases API: %v", err)
	}
	if api != "https://api.github.com/repos/sagernet/cronet-go/releases" {
		t.Errorf("releases API = %q", api)
	}
}

// A platform the block does not list needs no library. That is how the manifest
// says macOS is served by a sing-box with naiveproxy built in, and it has to
// beat the built-in fallback rather than be overridden by it.
func TestManifestNaiveBlockDecidesWhoNeedsNothing(t *testing.T) {
	m := parseManifest(t, manifestWithNaive)
	for _, c := range []struct{ goos, goarch string }{
		{"darwin", "arm64"},
		{"windows", "arm64"},
	} {
		if _, ok := cronetPlanFor(m, c.goos, c.goarch); ok {
			t.Errorf("%s-%s is not listed in the block and must install nothing", c.goos, c.goarch)
		}
	}
}

// A server that has not published the block yet leaves Windows working the way
// it did before there was one: upstream's latest, no pin.
func TestManifestWithoutNaiveFallsBackToLatest(t *testing.T) {
	m := parseManifest(t, `{"version": "v1.13.19"}`)
	if m.Naive != nil {
		t.Fatal("no block should parse as nil")
	}
	plan, ok := cronetPlanFor(m, "windows", "amd64")
	if !ok {
		t.Fatal("windows should still install a library without a block")
	}
	if plan.version != "" {
		t.Errorf("version = %q, want empty (latest) when nothing is pinned", plan.version)
	}
	if plan.assetName != "libcronet-windows-amd64.dll" || plan.installName != "libcronet.dll" {
		t.Errorf("fallback plan = %+v", plan)
	}
	// The same holds when the manifest could not be fetched at all.
	if _, ok := cronetPlanFor(nil, "windows", "amd64"); !ok {
		t.Error("a missing manifest should still fall back")
	}
	if _, ok := cronetPlanFor(nil, "darwin", "arm64"); ok {
		t.Error("darwin needs nothing, manifest or not")
	}
}

// The repository decides where a library is downloaded from and loaded into the
// process that runs the tunnel. It is read from the manifest, so it is checked.
func TestCronetReleasesAPIRejectsAnythingButGitHub(t *testing.T) {
	for _, repo := range []string{
		"https://evil.example/sagernet/cronet-go",
		"https://github.com.evil.example/sagernet/cronet-go",
		"http://github.com/sagernet/cronet-go/extra/path",
		"https://github.com/sagernet",
		"https://github.com/",
		"::not a url",
	} {
		if api, err := cronetReleasesAPI(repo); err == nil {
			t.Errorf("source_repo %q was accepted as %q", repo, api)
		}
	}
	// Empty falls back to the default repository rather than failing.
	api, err := cronetReleasesAPI("")
	if err != nil || !strings.HasPrefix(api, "https://api.github.com/repos/") {
		t.Errorf("empty repo = %q, %v", api, err)
	}
	// http is fine to *name* a repo with — the API URL built from it is https.
	if api, err := cronetReleasesAPI("http://github.com/sagernet/cronet-go"); err != nil || !strings.HasPrefix(api, "https://") {
		t.Errorf("http repo = %q, %v", api, err)
	}
}

// library_filename names a file in the sing-box directory. It must not be able
// to name a path out of it.
func TestNaivePlanRejectsAPathAsLibraryFilename(t *testing.T) {
	for _, name := range []string{
		"../libcronet.dll",
		"sub/libcronet.dll",
		"/etc/libcronet.dll",
		"..",
		".",
	} {
		n := &NaiveLibrary{
			Platforms:       []string{"windows-amd64"},
			LibraryFilename: name,
			SourceAsset:     "libcronet-windows-amd64.dll",
		}
		if _, ok := naivePlan(n, "windows", "amd64"); ok {
			t.Errorf("library_filename %q was accepted", name)
		}
	}
}

// A block that omits the optional fields still works: the asset and the install
// name fall back to what the client would have used on its own.
func TestNaivePlanFillsInWhatTheBlockOmits(t *testing.T) {
	n := &NaiveLibrary{Platforms: []string{"windows-amd64"}, CronetVersion: "150.0.7871.63"}
	plan, ok := naivePlan(n, "windows", "amd64")
	if !ok {
		t.Fatal("a block naming only a version should still resolve")
	}
	if plan.assetName != "libcronet-windows-amd64.dll" || plan.installName != "libcronet.dll" {
		t.Errorf("plan = %+v", plan)
	}
	if plan.version != "150.0.7871.63" {
		t.Errorf("version = %q", plan.version)
	}
	// A block with no platforms listed applies wherever an asset exists.
	if _, ok := naivePlan(&NaiveLibrary{CronetVersion: "1"}, "darwin", "arm64"); ok {
		t.Error("darwin has no asset and must resolve to nothing")
	}
	if _, ok := naivePlan(&NaiveLibrary{CronetVersion: "1"}, "windows", "amd64"); !ok {
		t.Error("windows should resolve when no platform list narrows it")
	}
}
