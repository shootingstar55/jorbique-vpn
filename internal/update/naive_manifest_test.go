package update

import (
	"encoding/json"
	"testing"
)

// manifestWithNaive is the block as the update server publishes it.
const manifestWithNaive = `{
  "version": "v1.15.0-alpha.10",
  "assets": {"windows-amd64": {"url": "https://example.invalid/sing-box.zip", "sha256": "aa", "format": "zip"}},
  "naive": {
    "platforms": ["windows-amd64"],
    "library_filename": "libcronet.dll",
    "cronet_version": "154.0.8037.49",
    "url": "https://example.invalid/chan/libcronet-windows-amd64.dll",
    "sha256": "8B005A933AC0D0899C047617A9CD18418810D9426D0919C824520094A2C893F5"
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
	want := cronetPlan{
		url:         "https://example.invalid/chan/libcronet-windows-amd64.dll",
		sha256:      "8b005a933ac0d0899c047617a9cd18418810d9426d0919c824520094a2c893f5",
		installName: "libcronet.dll",
		version:     "154.0.8037.49",
	}
	if plan != want {
		t.Errorf("plan = %+v, want %+v", plan, want)
	}
}

// A platform the block does not list needs no library. That is how the manifest
// says macOS is served by a sing-box with naiveproxy built in.
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

// Without a block Windows still needs the library, but nothing names one: the
// plan comes back without a url, and ensureCronet reports that. Other platforms
// need nothing, manifest or not.
func TestManifestWithoutNaiveNamesNoLibrary(t *testing.T) {
	m := parseManifest(t, `{"version": "v1.15.0-alpha.10"}`)
	if m.Naive != nil {
		t.Fatal("no block should parse as nil")
	}
	for name, manifest := range map[string]*SingboxManifest{"no block": m, "no manifest": nil} {
		plan, ok := cronetPlanFor(manifest, "windows", "amd64")
		if !ok || plan.url != "" || plan.installName != "libcronet.dll" {
			t.Errorf("%s: windows plan = %+v, %v; want a plan with no url", name, plan, ok)
		}
		if _, ok := cronetPlanFor(manifest, "darwin", "arm64"); ok {
			t.Errorf("%s: darwin needs nothing", name)
		}
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
		m := &SingboxManifest{Naive: &NaiveLibrary{
			Platforms:       []string{"windows-amd64"},
			LibraryFilename: name,
			URL:             "https://example.invalid/libcronet-windows-amd64.dll",
			SHA256:          "aa",
		}}
		if _, ok := cronetPlanFor(m, "windows", "amd64"); ok {
			t.Errorf("library_filename %q was accepted", name)
		}
	}
}

// A block that omits the optional fields still works: the install name falls
// back to the loader's, and a block with no platforms listed applies to Windows.
func TestNaivePlanFillsInWhatTheBlockOmits(t *testing.T) {
	m := &SingboxManifest{Naive: &NaiveLibrary{URL: "https://example.invalid/l.dll", SHA256: "aa"}}
	plan, ok := cronetPlanFor(m, "windows", "amd64")
	if !ok {
		t.Fatal("a block naming only the file should still resolve")
	}
	if plan.installName != "libcronet.dll" || plan.url != "https://example.invalid/l.dll" || plan.sha256 != "aa" {
		t.Errorf("plan = %+v", plan)
	}
	if _, ok := cronetPlanFor(m, "darwin", "arm64"); ok {
		t.Error("darwin loads no library and must resolve to nothing")
	}
}
