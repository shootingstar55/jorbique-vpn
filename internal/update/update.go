package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/httpsclient"
)

const manifestCacheTTL = 90 * time.Second

// maxSingboxDownloadBytes caps the sing-box payload read (256 MiB). Real builds
// are tens of MiB; the ceiling just stops a hostile server exhausting memory.
const maxSingboxDownloadBytes = 256 << 20

// maxSingboxExtractBytes caps how many bytes an archive may decompress to
// (512 MiB). The download itself is size-capped and checksum-verified, but a
// compressed payload can expand ~1000x, so extraction needs its own ceiling to
// keep a decompression bomb from filling disk or memory. A var so tests can
// lower it; real builds are tens of MiB, nowhere near the limit.
var maxSingboxExtractBytes int64 = 512 << 20

// errExtractTooLarge reports an archive that decompresses past
// maxSingboxExtractBytes — a decompression bomb, not a real sing-box build.
var errExtractTooLarge = errors.New("decompressed payload too large")

// copyLimited is io.Copy with a ceiling: it fails with errExtractTooLarge once
// src yields more than maxSingboxExtractBytes, so archive extraction cannot be
// used as a decompression bomb.
func copyLimited(dst io.Writer, src io.Reader) (int64, error) {
	limit := maxSingboxExtractBytes
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("%w (limit %d MiB)", errExtractTooLarge, limit>>20)
	}
	return n, nil
}

var (
	manifestMu     sync.Mutex
	cachedManifest *SingboxManifest
	cachedAt       time.Time
	cachedErr      error
)

// SingboxAsset is one platform build under manifest "assets" (e.g. windows-amd64).
type SingboxAsset struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Format   string `json:"format"`
	URL      string `json:"url"`
	Source   string `json:"source"`
}

// SingboxManifest matches the JSON at version.json (custom update server).
type SingboxManifest struct {
	Version  string                  `json:"version"`
	Filename string                  `json:"filename"`
	SHA256   string                  `json:"sha256"`
	URL      string                  `json:"url"`
	Assets   map[string]SingboxAsset `json:"assets"`
	// Naive names the cronet library the Windows `naive` outbound loads, which
	// the server publishes beside the sing-box archive; see cronetPlanFor.
	Naive *NaiveLibrary `json:"naive"`
}

func singboxFilenameForInstall() string {
	if runtime.GOOS == "windows" {
		return constants.ProcessExeName
	}
	return "sing-box"
}

// manifestPayload picks the download URL/hash for this OS/arch (assets.windows-amd64, darwin-arm64, etc.).
func manifestPayload(m *SingboxManifest) (downloadURL, sha256Hex, format string, err error) {
	if m == nil {
		return "", "", "", fmt.Errorf("manifest: nil")
	}
	key := ""
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			key = "windows-amd64"
		case "arm64":
			key = "windows-arm64"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			key = "darwin-amd64"
		case "arm64":
			key = "darwin-arm64"
		}
	}
	// Windows/macOS always use assets[<arch>]. Root url/sha256 in the manifest point at the Linux
	// build; never use them as a fallback when we already resolved an arch-specific key.
	if key != "" {
		if m.Assets == nil {
			return "", "", "", fmt.Errorf("manifest: missing assets for %s", key)
		}
		a, ok := m.Assets[key]
		if !ok || strings.TrimSpace(a.URL) == "" {
			return "", "", "", fmt.Errorf("manifest: no assets[%s] url", key)
		}
		return strings.TrimSpace(a.URL), strings.TrimSpace(a.SHA256), strings.ToLower(strings.TrimSpace(a.Format)), nil
	}
	if strings.TrimSpace(m.URL) != "" {
		return strings.TrimSpace(m.URL), strings.TrimSpace(m.SHA256), "", nil
	}
	return "", "", "", fmt.Errorf("manifest: missing url")
}

// isLoopbackHost reports whether host is the local machine, where cleartext
// http is acceptable (no remote MITM is possible). Used only to exempt local
// test servers from the https requirement on download URLs.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, "v")
}

func fetchManifestUncached() (*SingboxManifest, error) {
	manifestURL := constants.SingboxManifestURL
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(manifestURL)
	if err != nil {
		return nil, err
	}
	client := httpsclient.ForHost(u.Host, 30*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
	}
	var m SingboxManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest: missing version")
	}
	if _, _, _, err := manifestPayload(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// InvalidateManifestCache clears cached manifest (e.g. after installing a new build).
func InvalidateManifestCache() {
	manifestMu.Lock()
	cachedManifest = nil
	cachedErr = nil
	cachedAt = time.Time{}
	manifestMu.Unlock()
}

// GetManifest returns the remote manifest with short caching between UI polls.
func GetManifest() (*SingboxManifest, error) {
	manifestMu.Lock()
	if cachedManifest != nil && cachedErr == nil && time.Since(cachedAt) < manifestCacheTTL {
		m := cachedManifest
		manifestMu.Unlock()
		return m, nil
	}
	if cachedErr != nil && time.Since(cachedAt) < manifestCacheTTL {
		err := cachedErr
		manifestMu.Unlock()
		return nil, err
	}
	manifestMu.Unlock()

	m, err := fetchManifestUncached()

	manifestMu.Lock()
	cachedManifest = m
	cachedErr = err
	cachedAt = time.Now()
	manifestMu.Unlock()
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetRemoteSingboxVersion returns the normalized version from the update server (e.g. "1.13.13"), or "" on error.
func GetRemoteSingboxVersion() string {
	m, err := GetManifest()
	if err != nil || m == nil {
		return ""
	}
	return normalizeVersion(m.Version)
}

func verifySHA256(data []byte, wantHex string) error {
	wantHex = strings.TrimSpace(strings.ToLower(wantHex))
	if wantHex == "" {
		// sing-box runs as a privileged VPN process; refuse to install a payload the
		// server did not vouch for. The manifest must always carry a sha256.
		return fmt.Errorf("manifest: missing sha256 (refusing to install unverified sing-box)")
	}
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		return fmt.Errorf("invalid sha256 in manifest: %w", err)
	}
	sum := sha256.Sum256(data)
	if !bytes.Equal(sum[:], want) {
		return fmt.Errorf("sha256 mismatch")
	}
	return nil
}

func isZipPayload(data []byte) bool {
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 3 && data[3] == 4
}

func isGzipPayload(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func extractSingboxExeFromZip(data []byte, dataDir string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	exeName := singboxFilenameForInstall()
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower(exeName)) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open zip entry: %w", err)
		}
		destPath := filepath.Join(dataDir, exeName)
		tmpMode := os.FileMode(0o644)
		if runtime.GOOS != "windows" {
			tmpMode = 0o755
		}
		dst, err := os.OpenFile(destPath+".new", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, tmpMode)
		if err != nil {
			rc.Close()
			return "", fmt.Errorf("create exe: %w", err)
		}
		_, err = copyLimited(dst, rc)
		rc.Close()
		dst.Close()
		if err != nil {
			os.Remove(destPath + ".new")
			return "", fmt.Errorf("extract exe: %w", err)
		}
		if err := replaceExe(destPath, destPath+".new"); err != nil {
			_ = os.Remove(destPath + ".new")
			return "", err
		}
		return destPath, nil
	}
	return "", fmt.Errorf("%s not found in zip", exeName)
}

func replaceExe(dest, src string) error {
	if runtime.GOOS == "windows" {
		// Windows Rename cannot overwrite an existing file, so move the current
		// binary aside first. If the install fails, restore it — the user is never
		// left with no binary (the failure mode of the old Remove-then-Rename).
		bak := dest + ".old"
		_ = os.Remove(bak)
		if err := os.Rename(dest, bak); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("back up old exe: %w", err)
		}
		if err := os.Rename(src, dest); err != nil {
			_ = os.Rename(bak, dest) // roll back to the binary we moved aside
			return fmt.Errorf("install exe: %w", err)
		}
		_ = os.Remove(bak)
		return nil
	}
	// POSIX rename atomically replaces an existing destination in one step, so the
	// old binary is never gone before the new one is in place.
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("install exe: %w", err)
	}
	_ = os.Chmod(dest, 0o755)
	return nil
}

var errSingboxNotInTar = errors.New("sing-box binary not found in tar archive")

func extractSingboxFromTarGz(data []byte, dataDir string) (string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	exeName := singboxFilenameForInstall()
	want := strings.ToLower(exeName)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if strings.ToLower(base) != want {
			continue
		}
		return installTarBinary(tr, dataDir, exeName)
	}
	return "", errSingboxNotInTar
}

func writeRawExe(data []byte, dataDir string) (string, error) {
	destPath := filepath.Join(dataDir, singboxFilenameForInstall())
	tmp := destPath + ".new"
	mode := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		mode = 0o755
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return "", fmt.Errorf("write temp exe: %w", err)
	}
	if err := replaceExe(destPath, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return destPath, nil
}

func extractSingboxFromGzipRaw(data []byte, dataDir string) (string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	var body bytes.Buffer
	if _, err := copyLimited(&body, gr); err != nil {
		return "", fmt.Errorf("ungzip: %w", err)
	}
	return writeRawExe(body.Bytes(), dataDir)
}

// readAllWithProgress reads r to completion, reporting download percent via logFn
// as it goes (level "PROGRESS", message = integer percent 0..100). When total <= 0
// the size is unknown, so no percentage is emitted and callers fall back to an
// indeterminate indicator. A nil logFn just reads without reporting.
func readAllWithProgress(r io.Reader, total int64, logFn func(string, string)) ([]byte, error) {
	var buf bytes.Buffer
	if total > 0 {
		buf.Grow(int(total))
	}
	chunk := make([]byte, 64*1024)
	var read int64
	lastPct := -1
	for {
		n, rerr := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			read += int64(n)
			lastPct = reportReadProgress(read, total, lastPct, logFn)
		}
		if rerr == io.EOF {
			return buf.Bytes(), nil
		}
		if rerr != nil {
			return nil, rerr
		}
	}
}

// DownloadAndExtract downloads from manifestPayload URL and installs sing-box (sing-box.exe on Windows, sing-box on Unix).
func DownloadAndExtract(dataDir string, logFn func(string, string)) (string, error) {
	m, err := GetManifest()
	if err != nil {
		return "", fmt.Errorf("manifest: %w", err)
	}
	downloadURL, wantSHA, format, err := manifestPayload(m)
	if err != nil {
		return "", err
	}
	version := normalizeVersion(m.Version)
	if logFn != nil {
		logFn("Downloading sing-box "+version+" from update server", "INFO")
	}
	du, err := url.Parse(downloadURL)
	if err != nil {
		return "", err
	}
	// The downloaded payload is executed as a privileged VPN process; require TLS
	// for every real (remote) host so a tampered/cleartext URL can't deliver the
	// binary. Loopback is exempt (it can't be MITM'd) so local test servers work.
	if !strings.EqualFold(du.Scheme, "https") && !isLoopbackHost(du.Hostname()) {
		return "", fmt.Errorf("download URL must use https, got %q", du.Scheme)
	}
	client := httpsclient.ForHost(du.Host, 120*time.Second)
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	// Cap the read so a malicious or misbehaving server can't exhaust memory; a
	// sing-box build is tens of MiB, well under this ceiling.
	body, err := readAllWithProgress(io.LimitReader(resp.Body, maxSingboxDownloadBytes), resp.ContentLength, logFn)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if err := verifySHA256(body, wantSHA); err != nil {
		return "", err
	}
	exeFound, err := extractSingboxPayload(body, dataDir, format)
	if err != nil {
		return "", err
	}
	if logFn != nil {
		logFn("Sing-box "+version+" installed to "+exeFound, "SUCCESS")
	}
	InvalidateManifestCache()
	return exeFound, nil
}

// EnsureSingbox returns the installed sing-box path, downloading it from the
// update server when it is missing. sing-box is no longer embedded in the client
// binary — every client fetches it from the server on first use.
func EnsureSingbox(dataDir string, logFn func(string, string)) (string, error) {
	exePath := filepath.Join(dataDir, singboxFilenameForInstall())
	if _, err := os.Stat(exePath); err == nil {
		return exePath, nil
	}
	return DownloadAndExtract(dataDir, logFn)
}

func installTarBinary(tr io.Reader, dataDir, exeName string) (string, error) {
	destPath := filepath.Join(dataDir, exeName)
	tmp := destPath + ".new"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mode = 0o644
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	_, copyErr := copyLimited(f, tr)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("extract: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close temp: %w", closeErr)
	}
	if err := replaceExe(destPath, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return destPath, nil
}

func reportReadProgress(read, total int64, lastPct int, logFn func(string, string)) int {
	if logFn == nil || total <= 0 {
		return lastPct
	}
	pct := int(read * 100 / total)
	if pct != lastPct {
		logFn(strconv.Itoa(pct), "PROGRESS")
	}
	return pct
}

func extractSingboxPayload(body []byte, dataDir, format string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(format))
	var exeFound string
	var err error
	switch {
	case f == "zip" || f == "" && isZipPayload(body):
		exeFound, err = extractSingboxExeFromZip(body, dataDir)
	case f == "tar.gz" || f == "tgz":
		exeFound, err = extractSingboxFromTarGz(body, dataDir)
	case isGzipPayload(body):
		exeFound, err = extractSingboxFromTarGz(body, dataDir)
		if err != nil && errors.Is(err, errSingboxNotInTar) {
			exeFound, err = extractSingboxFromGzipRaw(body, dataDir)
		}
	default:
		exeFound, err = writeRawExe(body, dataDir)
	}
	return exeFound, err
}
