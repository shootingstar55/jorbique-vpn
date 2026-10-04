package update

import (
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
// dlopen's it. The Windows build loads libcronet.dll at runtime, and the
// sing-box download does not carry it, so the client installs it itself.
//
// The update server publishes the library beside the sing-box archive: the very
// file the Windows sing-box was compiled against, taken from the cronet-go Go
// module that sing-box pins. cronet-go's GitHub releases are not that file —
// they trail the module (sing-box 1.15.0-alpha.10 pins 154.0.8037.49, which has
// no release at all) — and the two must match: symbols are looked up by name,
// so a library from another build fails at start.
//
// cronet-go looks for it by a fixed name in the directory of the running
// executable (internal/cronet/loader_windows.go: `libcronet.dll`, then $PATH),
// so it is installed next to sing-box.exe under that name, whatever the server
// calls the file.
const (
	// CronetLibraryFileName is the name cronet-go's loader searches for.
	CronetLibraryFileName = "libcronet.dll"

	// cronetVersionFileName records the sha256 of the installed library, so a
	// later run can tell "already current" from "never fetched" without
	// hashing 10 MB on every connect.
	cronetVersionFileName = "libcronet.version"
)

// NaiveLibrary is the manifest's "naive" block: the cronet library the update
// server publishes for the sing-box beside it.
type NaiveLibrary struct {
	// Platforms are manifest asset keys ("windows-amd64"). A client whose
	// platform is not listed installs nothing — that is how the manifest says
	// macOS needs no library, its sing-box having naiveproxy built in.
	Platforms []string `json:"platforms"`
	// LibraryFilename is the name to install under. It is what cronet-go's
	// loader searches for, not what the server calls the file.
	LibraryFilename string `json:"library_filename"`
	// CronetVersion is the Chromium version of the build, e.g. "154.0.8037.49".
	// It is only shown in the log: the sha256 is what identifies the file.
	CronetVersion string `json:"cronet_version"`
	// URL is where the server publishes the library, and SHA256 its checksum.
	// The manifest arrives over the pinned channel, so the checksum is the
	// server's own word for the file, the same as for the sing-box archive.
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// cronetPlan is the resolved answer to "what should be on this machine": where
// to fetch the library, its checksum, and the name to install it under. An
// empty url means the manifest names no library for a platform that needs one.
type cronetPlan struct {
	url         string
	sha256      string
	installName string
	version     string
}

// platformKey is the manifest's name for this OS/arch, matching the keys under
// "assets" ("windows-amd64").
func platformKey(goos, goarch string) string {
	return goos + "-" + goarch
}

// cronetPlanFor decides what this platform should install; ok is false when it
// needs nothing.
//
// The manifest's naive block lists the platforms that load the library. With no
// block, or a block that lists none, that is Windows: the only sing-box here
// that loads one. A platform that needs the library while the manifest names
// none gets a plan without a url, which ensureCronet reports rather than
// guessing a source.
func cronetPlanFor(m *SingboxManifest, goos, goarch string) (cronetPlan, bool) {
	var n *NaiveLibrary
	if m != nil {
		n = m.Naive
	}
	if n == nil || len(n.Platforms) == 0 {
		if goos != "windows" {
			return cronetPlan{}, false
		}
	} else if !platformListed(n.Platforms, platformKey(goos, goarch)) {
		return cronetPlan{}, false
	}
	plan := cronetPlan{installName: CronetLibraryFileName}
	if n == nil {
		return plan, true
	}
	if name := strings.TrimSpace(n.LibraryFilename); name != "" {
		// A filename is all this may be. The manifest names what to call a file
		// in the sing-box directory, not where in the filesystem to put one.
		if name != filepath.Base(name) || name == "." || name == ".." {
			return cronetPlan{}, false
		}
		plan.installName = name
	}
	plan.url = strings.TrimSpace(n.URL)
	plan.sha256 = strings.ToLower(strings.TrimSpace(n.SHA256))
	plan.version = strings.TrimSpace(n.CronetVersion)
	return plan, true
}

func platformListed(platforms []string, want string) bool {
	for _, p := range platforms {
		if strings.EqualFold(strings.TrimSpace(p), want) {
			return true
		}
	}
	return false
}

// cronetLibraryPath is where the library belongs for a given sing-box directory.
func cronetLibraryPath(dataDir, installName string) string {
	if installName == "" {
		installName = CronetLibraryFileName
	}
	return filepath.Join(dataDir, installName)
}

// installedCronetSHA256 reports the checksum recorded for the library already
// in dataDir, or "" when either the library or its marker is missing. Both have
// to be there: a marker without the file it describes would skip the download.
func installedCronetSHA256(dataDir, installName string) string {
	if _, err := os.Stat(cronetLibraryPath(dataDir, installName)); err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dataDir, cronetVersionFileName))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}

// fetchCronetLibrary downloads the library and returns its verified bytes.
//
// The library is dlopen'd by sing-box, which runs the tunnel — a swapped
// payload is code execution inside the VPN process, so the same rules as the
// sing-box binary apply: https to any real host, and nothing is installed that
// does not match the checksum the manifest states.
func fetchCronetLibrary(rawURL, wantSHA string) ([]byte, error) {
	du, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(du.Scheme, "https") && !isLoopbackHost(du.Hostname()) {
		return nil, fmt.Errorf("download URL must use https, got %q", du.Scheme)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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
	if err := verifySHA256(body, wantSHA); err != nil {
		return nil, err
	}
	return body, nil
}

// installCronetLibrary writes the library and its checksum marker into dataDir.
// The replace is the same swap the sing-box binary uses, so a run that dies
// mid-write cannot leave a truncated .dll where sing-box will try to load one.
func installCronetLibrary(dataDir, installName, sha string, body []byte) (string, error) {
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
	if err := os.WriteFile(filepath.Join(dataDir, cronetVersionFileName), []byte(sha+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write version marker: %w", err)
	}
	return dest, nil
}

// ensureCronet installs what plan describes into dataDir and returns its path.
// It downloads only when the library on disk is not the one the plan names.
func ensureCronet(dataDir string, plan cronetPlan, logFn func(string, string)) (string, error) {
	dest := cronetLibraryPath(dataDir, plan.installName)
	_, statErr := os.Stat(dest)
	haveLibrary := statErr == nil
	if plan.url == "" || plan.sha256 == "" {
		return cronetFallback(haveLibrary, dest, fmt.Errorf("version.json names no library for this platform"),
			"version.json names no cronet library; keeping the installed one", logFn)
	}
	if haveLibrary && installedCronetSHA256(dataDir, plan.installName) == plan.sha256 {
		return dest, nil
	}

	label := plan.version
	if label == "" {
		label = "library"
	}
	if logFn != nil {
		logFn("Downloading cronet "+label+" for the naive outbound", "INFO")
	}
	body, err := fetchCronetLibrary(plan.url, plan.sha256)
	if err != nil {
		// The server being unreachable must not stop a connection that an
		// already installed library can serve. It is only fatal with nothing
		// on disk, where a profile using `naive` has no way to start.
		return cronetFallback(haveLibrary, dest, err,
			"cronet "+label+" download failed ("+err.Error()+"); keeping the installed library", logFn)
	}
	installed, err := installCronetLibrary(dataDir, plan.installName, plan.sha256, body)
	if err != nil {
		return "", fmt.Errorf(cronetErrorFormat, err)
	}
	if logFn != nil {
		logFn("cronet "+label+" installed to "+installed, "SUCCESS")
	}
	return installed, nil
}

// EnsureCronet installs the cronet library next to sing-box in dataDir, which is
// where sing-box looks for it, and returns its path.
//
// Which file is the manifest's call — see NaiveLibrary. It downloads only when
// what is installed is not already that file, so a library stops being
// re-checked once it is on disk.
//
// On a platform that needs no library — every non-Windows client, whose sing-box
// has naiveproxy built in — it does nothing and returns "", nil.
func EnsureCronet(dataDir string, logFn func(string, string)) (string, error) {
	m, err := GetManifest()
	return ensureCronetFrom(m, err, runtime.GOOS, runtime.GOARCH, dataDir, logFn)
}

// ensureCronetFrom is EnsureCronet with the manifest and the platform passed
// in, so the behaviour can be exercised for any of them.
func ensureCronetFrom(m *SingboxManifest, manifestErr error, goos, goarch, dataDir string, logFn func(string, string)) (string, error) {
	plan, ok := cronetPlanFor(m, goos, goarch)
	if !ok {
		return "", nil
	}
	if m == nil {
		// Nothing says which library to fetch. One already installed keeps
		// serving; the sing-box download in the same flow has already reported
		// the manifest error itself.
		dest := cronetLibraryPath(dataDir, plan.installName)
		_, statErr := os.Stat(dest)
		return cronetFallback(statErr == nil, dest, fmt.Errorf("version.json: %w", manifestErr),
			"Could not read version.json; keeping the installed cronet library", logFn)
	}
	return ensureCronet(dataDir, plan, logFn)
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
