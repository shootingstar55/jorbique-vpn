// Package clientupdate checks the official client release and stages a verified
// installer. VPN UUIDs and server credentials never go to GitHub.
package clientupdate

import (
	"context"
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
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/httpsclient"
	"jorbiquevpn/internal/semver"
)

const repository = "https://github.com/shootingstar55/jorbique-vpn"
const latestURL = "https://api.github.com/repos/shootingstar55/jorbique-vpn/releases/latest"
const maxPackageBytes = 256 << 20

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var installMu sync.Mutex
var client = newClient()
var platform = runtime.GOOS

type Status struct {
	CurrentVersion string `json:"currentVersion"`
	Version        string `json:"version"`
	Available      bool   `json:"available"`
}

type asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}
type packageInfo struct {
	version string
	asset   asset
}

func newClient() *http.Client {
	c := httpsclient.ForHost("api.github.com", 3*time.Minute)
	check := c.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := check(req, via); err != nil {
			return err
		}
		if !trustedURL(req.URL) {
			return errors.New("неизвестный сервер обновлений")
		}
		return nil
	}
	return c
}

func trustedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func assetName(version, platform string) string {
	suffix := map[string]string{"windows": "windows-" + version + ".zip", "darwin": "macOS-" + version + ".pkg", "android": "android-" + version + ".apk"}[platform]
	if suffix == "" {
		return ""
	}
	return "Jorbique-VPN-Extended-" + suffix
}

func latest(ctx context.Context) (packageInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return packageInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return packageInfo{}, errors.New("не удалось проверить обновление клиента")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return packageInfo{}, fmt.Errorf("сервер обновлений: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return packageInfo{}, errors.New("некорректный ответ сервера обновлений")
	}
	var r release
	if json.Unmarshal(body, &r) != nil {
		return packageInfo{}, errors.New("некорректный список обновлений")
	}
	v := strings.TrimPrefix(r.Tag, "v")
	if r.Tag != "v"+v || !versionPattern.MatchString(v) || r.Draft || r.Prerelease {
		return packageInfo{}, errors.New("некорректная версия обновления")
	}
	name := assetName(v, platform)
	for _, a := range r.Assets {
		if a.Name != name || name == "" {
			continue
		}
		hash, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
		if !strings.HasPrefix(a.Digest, "sha256:") || err != nil || len(hash) != sha256.Size || a.Size <= 0 || a.Size > maxPackageBytes {
			return packageInfo{}, errors.New("обновление не содержит корректной SHA-256 или размера")
		}
		return packageInfo{v, a}, nil
	}
	return packageInfo{}, errors.New("установщик для этой ОС ещё не опубликован")
}

func Check(ctx context.Context) (Status, error) {
	status := Status{CurrentVersion: constants.AppVersion}
	if !versionPattern.MatchString(constants.AppVersion) {
		return status, nil
	}
	p, err := latest(ctx)
	if err != nil {
		return status, err
	}
	status.Version = p.version
	status.Available = semver.Compare(p.version, constants.AppVersion) > 0
	return status, nil
}

// DownloadLatest resolves the release again: the UI cannot supply a URL, a hash,
// a destination or an older version to install.
func DownloadLatest(ctx context.Context) (string, error) {
	if !installMu.TryLock() {
		return "", errors.New("обновление уже выполняется")
	}
	defer installMu.Unlock()
	p, err := latest(ctx)
	if err != nil {
		return "", err
	}
	if !versionPattern.MatchString(constants.AppVersion) || semver.Compare(p.version, constants.AppVersion) <= 0 {
		return "", errors.New("новой версии клиента нет")
	}
	return download(ctx, p)
}

func download(ctx context.Context, p packageInfo) (string, error) {
	root := filepath.Join(os.TempDir(), "jorbique-client-updates")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, "release-")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, p.asset.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repository+"/releases/download/v"+p.version+"/"+p.asset.Name, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("не удалось скачать обновление клиента")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("скачивание обновления: HTTP %d", resp.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, p.asset.Size+1))
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if size != p.asset.Size || !strings.EqualFold("sha256:"+hex.EncodeToString(hash.Sum(nil)), p.asset.Digest) {
		return "", errors.New("проверка обновления не пройдена: размер или SHA-256 не совпали")
	}
	ok = true
	return path, nil
}
