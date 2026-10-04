package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"jorbiquevpn/internal/httpsclient"
)

const (
	cronetErrorFormat = "cronet: %w"
)

// The `naive` outbound in the sing-box profile is a naiveproxy client, and
// naiveproxy is Chromium's network stack: sing-box does not implement it, it
// dlopen's it. The library is published as a release asset of cronet-go rather
// than bundled with sing-box, so a client that offers `naive` has to fetch it
// itself.
//
// cronet-go looks for it by a fixed name in the directory of the running
// executable (internal/cronet/loader_windows.go: `libcronet.dll`, then $PATH),
// so the asset — which is published per-arch as libcronet-windows-amd64.dll —
// is installed next to sing-box.exe under the name the loader searches for.
// Renaming it is not cosmetic: sing-box will not find libcronet-windows-amd64.dll.
const (
	// CronetLibraryFileName is the name cronet-go's loader searches for.
	CronetLibraryFileName = "libcronet.dll"

	// cronetVersionFileName records which release the installed library came
	// from, so a later run can tell "already current" from "never fetched"
	// without hashing 9 MB.
	cronetVersionFileName = "libcronet.version"

	// cronetDefaultRepo is where the library is published when the manifest does
	// not name a repository.
	cronetDefaultRepo = "https://github.com/SagerNet/cronet-go"
)

// NaiveLibrary is the manifest's "naive" block: which build of the cronet
// library a client should be running, rather than whatever upstream released
// most recently.
//
// Pinning it here is the point. sing-box and cronet share an ABI, so the pair
// that was tested together is the pair that should be installed; tracking
// upstream's latest means a cronet release lands on the fleet the day it is
// published, against a sing-box build nobody has tried it with.
type NaiveLibrary struct {
	// Platforms are manifest asset keys ("windows-amd64"). A client whose
	// platform is not listed installs nothing — that is how the manifest says
	// macOS needs no library, its sing-box having naiveproxy built in.
	Platforms []string `json:"platforms"`
	// LibraryFilename is the name to install under. It is what cronet-go's
	// loader searches for, not what the asset is called.
	LibraryFilename string `json:"library_filename"`
	// CronetVersion is the Chromium version of the build to install, e.g.
	// "150.0.7871.63". Upstream's release tags carry a build suffix on top of
	// it, so this is matched against a tag rather than being one.
	CronetVersion string `json:"cronet_version"`
	SourceRepo    string `json:"source_repo"`
	SourceAsset   string `json:"source_asset"`
}

// cronetPlan is the resolved answer to "what should be on this machine": the
// asset to fetch, the name to install it under, and which version. An empty
// version means upstream's latest, which is what the client falls back to while
// the manifest carries no naive block.
type cronetPlan struct {
	assetName   string
	installName string
	version     string
	repo        string
}

// platformKey is the manifest's name for this OS/arch, matching the keys under
// "assets" ("windows-amd64").
func platformKey(goos, goarch string) string {
	return goos + "-" + goarch
}

// cronetTagMatches reports whether a release tag is the build of version.
//
// Upstream has not been consistent about tag shape — "v150.0.7871.63-1",
// "143.0.7499.109-2" and "v0.0.1-143.0.7499.109-1" are all real tags — so the
// version is matched as a whole hyphen-delimited component rather than by
// prefix, which would accept 150.0.7871.630 for 150.0.7871.63.
func cronetTagMatches(tag, version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	for _, part := range strings.Split(strings.TrimSpace(tag), "-") {
		if part == version || part == "v"+version {
			return true
		}
	}
	return false
}

// cronetReleasesAPI turns a repository URL into the GitHub API endpoint listing
// its releases.
//
// The repository comes out of the manifest, and the manifest is fetched over
// the pinned channel — but this still refuses anything that is not a GitHub
// repository URL. The result is where a library gets downloaded from and loaded
// into the process that runs the tunnel, so it is not a field to follow
// wherever it points.
func cronetReleasesAPI(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = cronetDefaultRepo
	}
	u, err := url.Parse(repo)
	if err != nil {
		return "", fmt.Errorf("source_repo %q: %w", repo, err)
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return "", fmt.Errorf("source_repo %q: only github.com is accepted", repo)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("source_repo %q: expected github.com/<owner>/<repo>", repo)
	}
	return "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/releases", nil
}

// naivePlan reads the manifest's naive block for this platform.
//
// ok is false when the manifest says this platform needs no library — either
// because it lists platforms and this is not one of them, or because the block
// names no asset for it.
func naivePlan(n *NaiveLibrary, goos, goarch string) (cronetPlan, bool) {
	if n == nil {
		return cronetPlan{}, false
	}
	if len(n.Platforms) > 0 {
		want := platformKey(goos, goarch)
		listed := false
		for _, p := range n.Platforms {
			if strings.EqualFold(strings.TrimSpace(p), want) {
				listed = true
				break
			}
		}
		if !listed {
			return cronetPlan{}, false
		}
	}
	asset := strings.TrimSpace(n.SourceAsset)
	if asset == "" {
		asset = cronetAssetName(goos, goarch)
	}
	if asset == "" {
		return cronetPlan{}, false
	}
	install := strings.TrimSpace(n.LibraryFilename)
	if install == "" {
		install = CronetLibraryFileName
	}
	// A filename is all this may be. The manifest names what to call a file in
	// the sing-box directory, not where in the filesystem to put one.
	if install != filepath.Base(install) || install == "." || install == ".." {
		return cronetPlan{}, false
	}
	return cronetPlan{
		assetName:   asset,
		installName: install,
		version:     strings.TrimSpace(n.CronetVersion),
		repo:        strings.TrimSpace(n.SourceRepo),
	}, true
}

// githubAsset is one file attached to a GitHub release.
type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	// Digest is GitHub's own checksum, formatted "sha256:<hex>". It is what
	// the downloaded library is verified against; see fetchCronetLibrary.
	Digest string `json:"digest"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// cronetAssetName is the release asset a platform needs, or "" for one that
// needs none. Only Windows does: the macOS build of sing-box has naiveproxy in
// it natively and loads nothing at runtime.
func cronetAssetName(goos, goarch string) string {
	if goos != "windows" {
		return ""
	}
	switch goarch {
	case "amd64", "arm64":
		return "libcronet-windows-" + goarch + ".dll"
	}
	return ""
}

// cronetLibraryPath is where the library belongs for a given sing-box directory.
func cronetLibraryPath(dataDir, installName string) string {
	if installName == "" {
		installName = CronetLibraryFileName
	}
	return filepath.Join(dataDir, installName)
}

// installedCronetVersion reports the release tag of the library already in
// dataDir, or "" when either the library or its marker is missing. Both have to
// be there: a marker without the file it describes would skip the download.
func installedCronetVersion(dataDir, installName string) string {
	if _, err := os.Stat(cronetLibraryPath(dataDir, installName)); err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dataDir, cronetVersionFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// getGitHubJSON decodes a GitHub API response into out.
func getGitHubJSON(apiURL string, out any) error {
	u, err := url.Parse(apiURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpsclient.ForHost(u.Host, 30*time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release API HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// resolveCronetRelease finds the release to install from.
//
// With a version it walks the release list for the tag carrying that version;
// without one it takes whatever upstream released last, which is what happens
// while the manifest names no version at all.
func resolveCronetRelease(releasesAPI, version string) (*githubRelease, error) {
	if strings.TrimSpace(version) == "" {
		var rel githubRelease
		if err := getGitHubJSON(releasesAPI+"/latest", &rel); err != nil {
			return nil, err
		}
		if strings.TrimSpace(rel.TagName) == "" {
			return nil, fmt.Errorf("release API: missing tag_name")
		}
		return &rel, nil
	}
	var releases []githubRelease
	if err := getGitHubJSON(releasesAPI+"?per_page=100", &releases); err != nil {
		return nil, err
	}
	// Newest first, as GitHub returns them, so a version rebuilt as -2 wins over
	// its own -1.
	for i := range releases {
		if cronetTagMatches(releases[i].TagName, version) {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("no release for cronet version %s", version)
}

func findCronetAsset(rel *githubRelease, name string) (githubAsset, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			if strings.TrimSpace(a.URL) == "" {
				return githubAsset{}, fmt.Errorf("release %s: asset %s has no download url", rel.TagName, name)
			}
			return a, nil
		}
	}
	return githubAsset{}, fmt.Errorf("release %s: no asset named %s", rel.TagName, name)
}

// fetchCronetLibrary downloads one asset and returns its verified bytes.
//
// The library is dlopen'd by sing-box, which runs the tunnel — a swapped
// payload is code execution inside the VPN process, so the same rule as the
// sing-box binary applies: nothing is installed that the publisher did not
// vouch for. GitHub states the checksum in the release metadata (`digest`), and
// that is the only statement available for this file; a release that omits it
// is refused rather than trusted on transport security alone.
func fetchCronetLibrary(a githubAsset) ([]byte, error) {
	du, err := url.Parse(a.URL)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(du.Scheme, "https") && !isLoopbackHost(du.Hostname()) {
		return nil, fmt.Errorf("download URL must use https, got %q", du.Scheme)
	}
	hexDigest := strings.TrimSpace(a.Digest)
	if !strings.HasPrefix(strings.ToLower(hexDigest), "sha256:") {
		return nil, fmt.Errorf("asset %s: no sha256 digest in the release metadata (refusing to install an unverified library)", a.Name)
	}
	hexDigest = hexDigest[len("sha256:"):]

	req, err := http.NewRequest(http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpsclient.ForHost(du.Host, 120*time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	body, err := readAllWithProgress(io.LimitReader(resp.Body, maxSingboxDownloadBytes), resp.ContentLength, nil)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if err := verifySHA256(body, hexDigest); err != nil {
		return nil, err
	}
	return body, nil
}

// installCronetLibrary writes the library and its version marker into dataDir.
// The replace is the same swap the sing-box binary uses, so a run that dies
// mid-write cannot leave a truncated .dll where sing-box will try to load one.
func installCronetLibrary(dataDir, installName, tag string, body []byte) (string, error) {
	dest := cronetLibraryPath(dataDir, installName)
	tmp := dest + ".new"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", fmt.Errorf("write temp library: %w", err)
	}
	if err := replaceExe(dest, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	// The marker is written after the library, never before: a crash between
	// the two costs one redundant download, the other order costs a load
	// failure that nothing retries.
	if err := os.WriteFile(filepath.Join(dataDir, cronetVersionFileName), []byte(tag+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write version marker: %w", err)
	}
	return dest, nil
}

// ensureCronet installs what plan describes, with the API endpoint injected so
// the behaviour can be exercised without reaching GitHub.
func ensureCronet(releasesAPI, dataDir string, plan cronetPlan, logFn func(string, string)) (string, error) {
	assetName := plan.assetName
	if assetName == "" {
		return "", nil
	}
	dest := cronetLibraryPath(dataDir, plan.installName)
	_, statErr := os.Stat(dest)
	haveLibrary := statErr == nil
	if haveLibrary && cronetTagMatches(installedCronetVersion(dataDir, plan.installName), plan.version) {
		return dest, nil
	}

	rel, err := resolveCronetRelease(releasesAPI, plan.version)
	if err != nil {
		// GitHub being unreachable must not stop a connection that an already
		// installed library can serve. It is only fatal with nothing on disk,
		// where a profile using `naive` has no way to start.
		return cronetFallback(haveLibrary, dest, err,
			"Could not resolve the cronet library ("+err.Error()+"); keeping the installed one", logFn)
	}
	if haveLibrary && installedCronetVersion(dataDir, plan.installName) == rel.TagName {
		return dest, nil
	}

	asset, err := findCronetAsset(rel, assetName)
	if err != nil {
		return cronetFallback(haveLibrary, dest, err,
			"cronet release "+rel.TagName+" has no "+assetName+"; keeping the installed library", logFn)
	}
	if logFn != nil {
		logFn("Downloading cronet "+rel.TagName+" ("+assetName+") for the naive outbound", "INFO")
	}
	body, err := fetchCronetLibrary(asset)
	if err != nil {
		return cronetFallback(haveLibrary, dest, err,
			"cronet "+rel.TagName+" download failed ("+err.Error()+"); keeping the installed library", logFn)
	}
	installed, err := installCronetLibrary(dataDir, plan.installName, rel.TagName, body)
	if err != nil {
		return "", fmt.Errorf(cronetErrorFormat, err)
	}
	if logFn != nil {
		logFn("cronet "+rel.TagName+" installed to "+installed, "SUCCESS")
	}
	return installed, nil
}

// cronetPlanFor decides what this platform should install.
//
// The manifest's naive block is the answer when it has one: it names the build
// that was tested with the sing-box beside it, and it is fetched over the pinned
// channel. Without a block the client falls back to upstream's latest release,
// which is what it did before the manifest could say — a server that has not
// published the block yet must not leave Windows with no library at all.
func cronetPlanFor(m *SingboxManifest, goos, goarch string) (cronetPlan, bool) {
	if m != nil {
		if plan, ok := naivePlan(m.Naive, goos, goarch); ok {
			return plan, true
		}
		// A block that exists and does not list this platform is an answer, not
		// a gap: it says this client needs no library.
		if m.Naive != nil {
			return cronetPlan{}, false
		}
	}
	asset := cronetAssetName(goos, goarch)
	if asset == "" {
		return cronetPlan{}, false
	}
	return cronetPlan{assetName: asset, installName: CronetLibraryFileName}, true
}

// EnsureCronet installs the cronet library next to sing-box in dataDir, which is
// where sing-box looks for it, and returns its path.
//
// Which build is the manifest's call — see NaiveLibrary. It downloads only when
// what is installed is not already that build, so a pinned version stops being
// re-checked once it is on disk.
//
// On a platform that needs no library — every non-Windows client, whose sing-box
// has naiveproxy built in — it does nothing and returns "", nil.
func EnsureCronet(dataDir string, logFn func(string, string)) (string, error) {
	// A manifest error is not reported: the sing-box download in the same flow
	// has already surfaced it, and the fallback plan still installs a library.
	m, _ := GetManifest()
	plan, ok := cronetPlanFor(m, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return "", nil
	}
	releasesAPI, err := cronetReleasesAPI(plan.repo)
	if err != nil {
		return "", fmt.Errorf(cronetErrorFormat, err)
	}
	return ensureCronet(releasesAPI, dataDir, plan, logFn)
}

func cronetFallback(haveLibrary bool, dest string, err error, warning string, logFn func(string, string)) (string, error) {
	if !haveLibrary {
		return "", fmt.Errorf(cronetErrorFormat, err)
	}
	if logFn != nil {
		logFn(warning, "WARN")
	}
	return dest, nil
}
