package clientupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jorbiquevpn/internal/constants"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixture(t *testing.T, payload []byte) *release {
	t.Helper()
	oldClient, oldPlatform, oldVersion := client, platform, constants.AppVersion
	platform, constants.AppVersion = "windows", "1.0.0"
	t.Cleanup(func() { client, platform, constants.AppVersion = oldClient, oldPlatform, oldVersion })
	t.Setenv("TMPDIR", t.TempDir())
	hash := sha256.Sum256(payload)
	r := &release{Tag: "v1.1.0", Assets: []asset{{Name: assetName("1.1.0", "windows"), Size: int64(len(payload)), Digest: "sha256:" + hex.EncodeToString(hash[:])}}}
	client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" || req.Header.Get("X-Jorbique-Device") != "" {
			t.Fatal("VPN credentials leaked")
		}
		body := payload
		if req.URL.String() == latestURL {
			body, _ = json.Marshal(r)
		} else if req.URL.String() != repository+"/releases/download/v1.1.0/"+r.Assets[0].Name {
			t.Fatalf("unexpected URL: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	return r
}

func TestCheckAndVerifiedDownload(t *testing.T) {
	payload := []byte("official package")
	fixture(t, payload)
	status, err := Check(context.Background())
	if err != nil || !status.Available || status.Version != "1.1.0" || status.CurrentVersion != "1.0.0" {
		t.Fatalf("status %+v: %v", status, err)
	}
	path, err := DownloadLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(path))
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, payload) {
		t.Fatal("wrong payload")
	}
	if got := GetProgress(); got.Stage != "ready" || got.Downloaded != int64(len(payload)) || got.Total != int64(len(payload)) {
		t.Fatalf("verified download progress: %+v", got)
	}
	constants.AppVersion = "1.1.0"
	if _, err := DownloadLatest(context.Background()); err == nil {
		t.Fatal("must not install the same version")
	}
	constants.AppVersion = "2.0.0"
	if _, err := DownloadLatest(context.Background()); err == nil {
		t.Fatal("downgrade accepted")
	}
}

func TestDownloadProgressCountsWrittenBytes(t *testing.T) {
	progressMu.Lock()
	progress = Progress{Stage: "downloading", Total: 10}
	progressMu.Unlock()
	w := downloadProgress{}
	for _, part := range []string{"abc", "de"} {
		if n, err := w.Write([]byte(part)); err != nil || n != len(part) {
			t.Fatal(n, err)
		}
	}
	if got := GetProgress(); got.Stage != "downloading" || got.Downloaded != 5 || got.Total != 10 {
		t.Fatalf("mid-download progress: %+v", got)
	}
	setStage("verifying")
	if got := GetProgress(); got.Stage != "verifying" || got.Downloaded != 5 {
		t.Fatalf("verification lost byte count: %+v", got)
	}
}

func TestRejectUnverifiableReleases(t *testing.T) {
	for _, kind := range []string{"draft", "prerelease", "version", "tag", "digest", "size", "large", "name"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t, []byte("package"))
			switch kind {
			case "draft":
				r.Draft = true
			case "prerelease":
				r.Prerelease = true
			case "version":
				r.Tag = "v1.1.0/../../evil"
			case "tag":
				r.Tag = "1.1.0"
			case "digest":
				r.Assets[0].Digest = ""
			case "size":
				r.Assets[0].Size = 0
			case "large":
				r.Assets[0].Size = maxPackageBytes + 1
			case "name":
				r.Assets[0].Name = "../client.exe"
			}
			if _, err := Check(context.Background()); err == nil {
				t.Fatal("unsafe release accepted")
			}
		})
	}
}

func TestRejectCorruptAndTruncatedPackages(t *testing.T) {
	for _, kind := range []string{"hash", "truncated", "long"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t, []byte("package"))
			switch kind {
			case "hash":
				r.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
			case "truncated":
				r.Assets[0].Size++
			case "long":
				r.Assets[0].Size--
			}
			if _, err := DownloadLatest(context.Background()); err == nil {
				t.Fatal("corrupt package accepted")
			}
			files, _ := filepath.Glob(filepath.Join(os.TempDir(), "jorbique-client-updates", "release-*"))
			if len(files) != 0 {
				t.Fatal("failed download was retained", files)
			}
		})
	}
}

func TestDownloadRedirectsStayOnGitHubTLS(t *testing.T) {
	c := newClient()
	origin, _ := http.NewRequest("GET", repository, nil)
	for raw, want := range map[string]bool{
		"https://release-assets.githubusercontent.com/file?signature=abc": true,
		"https://objects.githubusercontent.com/file":                      true,
		"http://github.com/evil":                                          false,
		"https://github.com.evil.example/file":                            false,
		"https://evil.githubusercontent.com/file":                         false,
		"https://user:password@github.com/file":                           false,
		"https://github.com:444/file":                                     false,
	} {
		u, _ := url.Parse(raw)
		req := &http.Request{URL: u}
		if got := c.CheckRedirect(req, []*http.Request{origin}) == nil; got != want {
			t.Errorf("%s: allowed=%v", raw, got)
		}
	}
}

func TestSupportedAssetNames(t *testing.T) {
	for p, suffix := range map[string]string{"windows": "windows-1.2.3.zip", "darwin": "macOS-1.2.3.pkg", "android": "android-1.2.3.apk"} {
		if got := assetName("1.2.3", p); got != "Jorbique-VPN-Extended-"+suffix {
			t.Fatal(got)
		}
	}
	if assetName("1.2.3", "unknown") != "" {
		t.Fatal("unknown platform")
	}
}
