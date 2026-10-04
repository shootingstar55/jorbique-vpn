package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"jorbiquevpn/internal/constants"
)

func TestSingboxFilenameForInstall(t *testing.T) {
	got := singboxFilenameForInstall()
	if runtime.GOOS == "windows" {
		if got != constants.ProcessExeName {
			t.Errorf("windows filename = %q", got)
		}
	} else if got != "sing-box" {
		t.Errorf("unix filename = %q", got)
	}
}

func TestNormalizeVersion(t *testing.T) {
	for in, want := range map[string]string{"v1.2.3": "1.2.3", " 1.2.3 ": "1.2.3", "1.2.3": "1.2.3"} {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManifestPayload(t *testing.T) {
	if _, _, _, err := manifestPayload(nil); err == nil {
		t.Error("nil manifest should error")
	}

	// The asset key for the host platform.
	key := runtime.GOOS + "-" + runtime.GOARCH
	m := &SingboxManifest{
		Version: "1.0.0",
		Assets:  map[string]SingboxAsset{key: {URL: "https://host/sb.zip", SHA256: "ab", Format: "ZIP"}},
	}
	url, sha, format, err := manifestPayload(m)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if err != nil || url != "https://host/sb.zip" || sha != "ab" || format != "zip" {
			t.Fatalf("payload = %q %q %q %v", url, sha, format, err)
		}
		// Missing assets map -> error.
		if _, _, _, err := manifestPayload(&SingboxManifest{Version: "1"}); err == nil {
			t.Error("missing assets should error")
		}
		// Assets present but key absent -> error.
		if _, _, _, err := manifestPayload(&SingboxManifest{Version: "1", Assets: map[string]SingboxAsset{"other": {URL: "x"}}}); err == nil {
			t.Error("absent key should error")
		}
	} else {
		// On other platforms the root url is used as a fallback.
		_ = url
		_ = err
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("hello sing-box")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	if err := verifySHA256(data, ""); err == nil {
		t.Error("empty hash must be rejected (unverified payload)")
	}
	if err := verifySHA256(data, "   "); err == nil {
		t.Error("whitespace-only hash must be rejected (unverified payload)")
	}
	if err := verifySHA256(data, "  "+good+"  "); err != nil {
		t.Errorf("matching hash should pass: %v", err)
	}
	if err := verifySHA256(data, "deadbeef"); err == nil {
		t.Error("mismatched hash should fail")
	}
	if err := verifySHA256(data, "nothex!!"); err == nil {
		t.Error("invalid hex should fail")
	}
}

func TestPayloadSniffers(t *testing.T) {
	if !isZipPayload([]byte{'P', 'K', 3, 4, 0}) {
		t.Error("zip magic not detected")
	}
	if isZipPayload([]byte{'P', 'K'}) {
		t.Error("short buffer should not be a zip")
	}
	if !isGzipPayload([]byte{0x1f, 0x8b, 0}) {
		t.Error("gzip magic not detected")
	}
	if isGzipPayload([]byte{0x1f}) {
		t.Error("short buffer should not be gzip")
	}
}

func makeZip(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeGzip(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractSingboxExeFromZip(t *testing.T) {
	dir := t.TempDir()
	exe := singboxFilenameForInstall()
	zipData := makeZip(t, "release/"+exe, []byte("BINARY"))
	got, err := extractSingboxExeFromZip(zipData, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, exe) {
		t.Errorf("exe path = %q", got)
	}
	body, _ := os.ReadFile(got)
	if string(body) != "BINARY" {
		t.Errorf("extracted content = %q", body)
	}

	// Zip without the binary -> error.
	if _, err := extractSingboxExeFromZip(makeZip(t, "readme.txt", []byte("x")), dir); err == nil {
		t.Error("zip without binary should error")
	}
	// Not a zip -> error.
	if _, err := extractSingboxExeFromZip([]byte("nope"), dir); err == nil {
		t.Error("invalid zip should error")
	}
}

// makeMultiZip builds a zip with several entries in declaration order.
func makeMultiZip(t *testing.T, entries map[string][]byte, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractSingboxExeFromZipNestedLayout locks the behaviour real release archives rely on: the
// binary is found by filename suffix even when nested under a versioned directory and preceded by
// unrelated decoy entries (README, LICENSE), without those decoys being mistaken for the binary.
func TestExtractSingboxExeFromZipNestedLayout(t *testing.T) {
	dir := t.TempDir()
	exe := singboxFilenameForInstall()
	nested := "sing-box-1.13.7-darwin-arm64/" + exe
	entries := map[string][]byte{
		"sing-box-1.13.7-darwin-arm64/README.md": []byte("docs"),
		"sing-box-1.13.7-darwin-arm64/LICENSE":   []byte("mit"),
		nested:                                   []byte("NESTEDBIN"),
	}
	order := []string{
		"sing-box-1.13.7-darwin-arm64/README.md",
		"sing-box-1.13.7-darwin-arm64/LICENSE",
		nested,
	}
	got, err := extractSingboxExeFromZip(makeMultiZip(t, entries, order), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, exe) {
		t.Errorf("exe path = %q", got)
	}
	if body, _ := os.ReadFile(got); string(body) != "NESTEDBIN" {
		t.Errorf("extracted decoy instead of the binary: %q", body)
	}
}

func TestExtractSingboxFromTarGz(t *testing.T) {
	dir := t.TempDir()
	exe := singboxFilenameForInstall()
	data := makeTarGz(t, "sing-box-1.0/"+exe, []byte("TARBIN"))
	got, err := extractSingboxFromTarGz(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(got); string(body) != "TARBIN" {
		t.Errorf("extracted = %q", body)
	}

	// Tar.gz without the binary -> sentinel error.
	if _, err := extractSingboxFromTarGz(makeTarGz(t, "other.txt", []byte("x")), dir); err != errSingboxNotInTar {
		t.Errorf("expected errSingboxNotInTar, got %v", err)
	}
	// Not gzip -> error.
	if _, err := extractSingboxFromTarGz([]byte("nope"), dir); err == nil {
		t.Error("invalid gzip should error")
	}
}

func TestExtractSingboxFromGzipRawAndWriteRaw(t *testing.T) {
	dir := t.TempDir()
	exe := singboxFilenameForInstall()

	got, err := extractSingboxFromGzipRaw(makeGzip(t, []byte("RAWGZ")), dir)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(got); string(body) != "RAWGZ" {
		t.Errorf("gzip-raw extracted = %q", body)
	}
	if filepath.Base(got) != exe {
		t.Errorf("gzip-raw path = %q", got)
	}

	got, err = writeRawExe([]byte("PLAINEXE"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(got); string(body) != "PLAINEXE" {
		t.Errorf("raw exe = %q", body)
	}

	// Invalid gzip -> error.
	if _, err := extractSingboxFromGzipRaw([]byte("nope"), dir); err == nil {
		t.Error("invalid gzip-raw should error")
	}
}

// TestExtractRejectsDecompressionBomb locks the extraction ceiling: every
// archive path must fail with errExtractTooLarge once the decompressed payload
// exceeds maxSingboxExtractBytes, without installing a binary or leaving a
// partial .new file behind.
func TestExtractRejectsDecompressionBomb(t *testing.T) {
	origLimit := maxSingboxExtractBytes
	maxSingboxExtractBytes = 1 << 20 // 1 MiB so test payloads stay small
	t.Cleanup(func() { maxSingboxExtractBytes = origLimit })

	exe := singboxFilenameForInstall()
	// Compresses to a few KiB but inflates past the lowered limit.
	bomb := bytes.Repeat([]byte{0}, 2<<20)

	checkNothingInstalled := func(t *testing.T, dir string) {
		t.Helper()
		for _, name := range []string{exe + ".new", exe} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				t.Errorf("%s left behind after rejected extraction", name)
			} else if !os.IsNotExist(err) {
				t.Errorf("stat %s: %v", name, err)
			}
		}
	}

	t.Run("zip", func(t *testing.T) {
		dir := t.TempDir()
		_, err := extractSingboxExeFromZip(makeZip(t, "release/"+exe, bomb), dir)
		if !errors.Is(err, errExtractTooLarge) {
			t.Fatalf("expected errExtractTooLarge, got %v", err)
		}
		checkNothingInstalled(t, dir)
	})

	t.Run("tar.gz", func(t *testing.T) {
		dir := t.TempDir()
		_, err := extractSingboxFromTarGz(makeTarGz(t, "sing-box-1.0/"+exe, bomb), dir)
		if !errors.Is(err, errExtractTooLarge) {
			t.Fatalf("expected errExtractTooLarge, got %v", err)
		}
		checkNothingInstalled(t, dir)
	})

	t.Run("gzip raw", func(t *testing.T) {
		dir := t.TempDir()
		_, err := extractSingboxFromGzipRaw(makeGzip(t, bomb), dir)
		if !errors.Is(err, errExtractTooLarge) {
			t.Fatalf("expected errExtractTooLarge, got %v", err)
		}
		checkNothingInstalled(t, dir)
	})

	// A payload exactly at the limit must still extract: the cap may only
	// reject bombs, never legitimate builds.
	t.Run("at limit extracts", func(t *testing.T) {
		dir := t.TempDir()
		exact := bytes.Repeat([]byte{1}, int(maxSingboxExtractBytes))
		got, err := extractSingboxExeFromZip(makeZip(t, exe, exact), dir)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(got)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() != maxSingboxExtractBytes {
			t.Errorf("extracted size = %d, want %d", fi.Size(), maxSingboxExtractBytes)
		}
	})
}

func TestReplaceExe(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "sing-box")
	src := dest + ".new"
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	// dest does not exist yet: replaceExe just renames.
	if err := replaceExe(dest, src); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(dest); string(body) != "NEW" {
		t.Errorf("dest = %q", body)
	}
	// Replace an existing dest.
	if err := os.WriteFile(src, []byte("NEWER"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExe(dest, src); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(dest); string(body) != "NEWER" {
		t.Errorf("replaced dest = %q", body)
	}
}

func TestGetManifestCacheAndInvalidate(t *testing.T) {
	InvalidateManifestCache()
	defer InvalidateManifestCache()

	m := &SingboxManifest{Version: "v1.13.7"}
	manifestMu.Lock()
	cachedManifest = m
	cachedErr = nil
	cachedAt = time.Now()
	manifestMu.Unlock()

	got, err := GetManifest()
	if err != nil || got != m {
		t.Fatalf("cached manifest not returned: %v %v", got, err)
	}
	if v := GetRemoteSingboxVersion(); v != "1.13.7" {
		t.Errorf("remote version = %q", v)
	}

	// Cached error branch.
	manifestMu.Lock()
	cachedManifest = nil
	cachedErr = errTest
	cachedAt = time.Now()
	manifestMu.Unlock()
	if _, err := GetManifest(); err == nil {
		t.Error("cached error should be returned")
	}
	if GetRemoteSingboxVersion() != "" {
		t.Error("version should be empty on manifest error")
	}

	InvalidateManifestCache()
	manifestMu.Lock()
	cleared := cachedManifest == nil && cachedErr == nil
	manifestMu.Unlock()
	if !cleared {
		t.Error("InvalidateManifestCache did not clear state")
	}
}

var errTest = &manifestErr{}

type manifestErr struct{}

func (*manifestErr) Error() string { return "test manifest error" }

// errAfterReader yields okBytes of data and then returns errMid, simulating a
// connection that drops partway through the download.
type errAfterReader struct {
	data   []byte
	off    int
	errMid error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, r.errMid
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func TestReadAllWithProgress(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 256*1024) // > one 64KiB chunk so progress steps

	// Known total: bytes returned intact and progress climbs to 100.
	t.Run("known total reports progress", func(t *testing.T) {
		var pcts []int
		got, err := readAllWithProgress(bytes.NewReader(payload), int64(len(payload)), func(msg, level string) {
			if level != "PROGRESS" {
				t.Errorf("unexpected level %q", level)
			}
			n, _ := strconv.Atoi(msg)
			pcts = append(pcts, n)
		})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("body mismatch: got %d bytes", len(got))
		}
		if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
			t.Errorf("expected progress ending at 100, got %v", pcts)
		}
		for i := 1; i < len(pcts); i++ {
			if pcts[i] < pcts[i-1] {
				t.Errorf("progress went backwards: %v", pcts)
			}
		}
	})

	// Unknown size (total <= 0): full body read, no percentage callbacks.
	t.Run("unknown total emits no progress", func(t *testing.T) {
		called := false
		got, err := readAllWithProgress(bytes.NewReader(payload), 0, func(string, string) { called = true })
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("body mismatch: got %d bytes", len(got))
		}
		if called {
			t.Error("no progress should be emitted when total is unknown")
		}
	})

	// nil logFn: still reads to completion.
	t.Run("nil logFn", func(t *testing.T) {
		got, err := readAllWithProgress(bytes.NewReader(payload), int64(len(payload)), nil)
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("nil logFn read failed: %v", err)
		}
	})

	// Mid-stream error is propagated.
	t.Run("mid-stream error", func(t *testing.T) {
		errMid := errors.New("connection reset")
		r := &errAfterReader{data: payload, errMid: errMid}
		_, err := readAllWithProgress(r, int64(len(payload)), nil)
		if !errors.Is(err, errMid) {
			t.Errorf("expected mid-stream error, got %v", err)
		}
	})
}

var _ io.Reader = (*errAfterReader)(nil)
