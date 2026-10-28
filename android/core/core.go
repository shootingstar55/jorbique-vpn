// Package core is the Android client's engine and backend, bound to Kotlin
// with gomobile. It reuses the desktop client's logic (profile download,
// connection check, config build, account check, IP card) and runs
// sing-box-extended in process through libbox.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/experimental/libbox"

	"jorbiquevpn/internal/account"
	"jorbiquevpn/internal/clientupdate"
	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/ipinfo"
	"jorbiquevpn/internal/logging"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/security"
	"jorbiquevpn/internal/server"
	"jorbiquevpn/internal/telemetry"
)

const controllerLogFile = "controller.log"

var (
	initOnce sync.Once
	initErr  error
	dataDir  string
	storeMu  sync.Mutex
	store    *server.Store
)

// Setup prepares the backend once per process; the activity and the VPN
// service both call it. filesDir and cacheDir are the app's private
// directories. fixAndroidStack works around golang/go#68760 on the Android
// versions the official client applies it to.
func Setup(filesDir, cacheDir string, fixAndroidStack bool) error {
	initOnce.Do(func() { initErr = initialize(filesDir, cacheDir, fixAndroidStack) })
	return initErr
}

// SetDeviceInfo is what the metrics call this phone: its name in Settings
// (or maker and model) and the Android release and build. Go reads neither
// on Android, where the hostname is "localhost" on every phone. Call it
// before Setup.
func SetDeviceInfo(name, androidVersion, buildID string) {
	telemetry.SetDevice(name, "Android", androidVersion, buildID)
}

func initialize(filesDir, cacheDir string, fixAndroidStack bool) error {
	// The desktop packages find their directories through these: APPDATA for
	// the data directory, the temp directory for the profile cache.
	_ = os.Setenv("APPDATA", filesDir)
	_ = os.Setenv("TMPDIR", cacheDir)
	dataDir = paths.DataDir()
	if err := paths.EnsureDataDirs(); err != nil {
		return err
	}
	logging.Initialize(controllerLogFile, dataDir)
	// Logs are capped by size here as on the desktop: nothing else trims them.
	logging.CapLogsEvery(filepath.Join(dataDir, "logs"))
	loaded, err := server.Load(dataDir)
	if err != nil {
		logf(err.Error()+" — starting with empty settings", logging.LevelWarn)
		loaded = &server.Store{}
	}
	store = loaded
	engineDir := filepath.Join(dataDir, "engine")
	if err := libbox.Setup(&libbox.SetupOptions{
		BasePath:          engineDir,
		WorkingPath:       engineDir,
		TempPath:          filepath.Join(cacheDir, "engine"),
		FixAndroidStack:   fixAndroidStack,
		LogMaxLines:       3000,
		CrashReportSource: "android",
	}); err != nil {
		logf("engine setup: "+err.Error(), logging.LevelWarn)
	}
	_ = libbox.SetLocale("ru")
	telemetry.Start(dataDir, store.GetUserUUID, tunnelStates, libbox.Version,
		func(msg string) { logf(msg, "INFO") })
	go func() { _, _ = config.FetchSingboxClientJSONOrCache(nil) }()
	return nil
}

func ready() error {
	if store == nil {
		return errors.New("backend is not initialised")
	}
	return nil
}

func tunnelStates() map[string]bool { return map[string]bool{"sing-box": IsConnected()} }

func logPath(name string) string { return filepath.Join(dataDir, "logs", name) }

// hasUserUUID reports whether an account is stored; until one is, the app
// shows only the activation screen and Connect refuses.
func hasUserUUID() bool {
	return store != nil && security.ValidUUID(store.GetUserUUID(), false)
}

// Call is the window's backend: the Vue interface calls the desktop
// client's method names, and the Kotlin bridge passes them here with the
// arguments as a JSON array. The answer is JSON. Methods that need Android
// itself (connecting through the VpnService, the app list) are
// answered in Kotlin and never reach this.
func Call(method, argsJSON string) (string, error) {
	if err := ready(); err != nil {
		return "", err
	}
	var args []json.RawMessage
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("bad arguments for %s: %w", method, err)
		}
	}
	result, err := dispatch(method, args)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func dispatch(method string, args []json.RawMessage) (any, error) {
	switch method {
	case "HasUserUUID", "HasVpnUserConfig":
		return hasUserUUID(), nil
	case "GetUserUUID":
		return store.GetUserUUID(), nil
	case "ActivateUserUUID", "SetUserUUID":
		return nil, setUserUUID(stringArg(args, 0))
	case "ImportUserUUID":
		uuid := stringArg(args, 0)
		return nil, account.Import(context.Background(), uuid, func() error {
			if strings.EqualFold(strings.TrimSpace(uuid), store.GetUserUUID()) {
				return nil
			}
			return setUserUUID(uuid)
		})
	case "IsConnected":
		return IsConnected(), nil
	case "CancelConnect":
		CancelConnect()
		return nil, nil
	case "GetConnectionProgress":
		return connectionProgress(), nil
	case "GetConnectionReport":
		return connectionReport(), nil
	case "GetProfileProtocols":
		return nonNil(config.AllProxyTagsFresh()), nil
	case "GetAvailableOutbounds":
		return nonNil(config.AvailableProxyTagsFresh()), nil
	case "GetIPInfo":
		return ipInfoCache.Get(context.Background(), boolArg(args, 0))
	case "GetDirectDomains":
		return strings.Join(store.GetDirectDomains(), "\n"), nil
	case "SetDirectDomains":
		return nil, setDirectDomains(stringArg(args, 0))
	case "GetIgnoredProcessNames":
		return nonNil(store.GetIgnoredProcessNames()), nil
	case "GetIgnoredProcessPaths":
		// Android excludes apps by package; there are no executables to name.
		return []string{}, nil
	case "SetIgnoredProcessNamesAndPaths":
		return nil, setExcludedPackages(stringsArg(args, 0))
	case "ListLogFiles":
		return listLogFiles()
	case "GetLogContent":
		return logContent(stringArg(args, 0))
	case "ClearLogs":
		logging.ClearAll(dataDir)
		return nil, nil
	case "CheckClientUpdate":
		return clientupdate.Check(context.Background())
	case "GetClientUpdateProgress":
		return clientupdate.GetProgress(), nil
	case "DownloadClientUpdate":
		return clientupdate.DownloadLatest(context.Background())
	case "GetAccount":
		return account.Open(context.Background(), store.GetUserUUID())
	case "BindThisDevice":
		return account.CheckIn(context.Background(), store.GetUserUUID(), true)
	case "UnbindDevice":
		return account.Unbind(context.Background(), store.GetUserUUID(), stringArg(args, 0))
	case "GetSingboxLocalVersion":
		return libbox.Version(), nil
	case "GetAppVersion":
		return constants.AppVersion, nil
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func stringArg(args []json.RawMessage, i int) string {
	var s string
	if i < len(args) {
		_ = json.Unmarshal(args[i], &s)
	}
	return s
}

func boolArg(args []json.RawMessage, i int) bool {
	var b bool
	if i < len(args) {
		_ = json.Unmarshal(args[i], &b)
	}
	return b
}

func stringsArg(args []json.RawMessage, i int) []string {
	var s []string
	if i < len(args) {
		_ = json.Unmarshal(args[i], &s)
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// verifyTimeout bounds the account check, as on the desktop.
const verifyTimeout = 20 * time.Second

// setUserUUID stores an account once the server knows it: the metrics are
// counted by it, so a UUID the server does not have cannot be saved.
func setUserUUID(value string) error {
	uuid := strings.TrimSpace(value)
	if uuid == "" {
		return errors.New("Введите UUID")
	}
	if !security.ValidUUID(uuid, false) {
		return errors.New("Неверный формат UUID")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	if !strings.EqualFold(uuid, strings.TrimSpace(store.GetUserUUID())) {
		ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
		err := telemetry.Verify(ctx, dataDir, uuid, tunnelStates, libbox.Version)
		cancel()
		switch {
		case err == nil:
		case errors.Is(err, telemetry.ErrUnknownUUID):
			return errors.New("Такого пользователя не существует")
		case errors.Is(err, telemetry.ErrVerificationUnavailable):
			return errors.New("Сервер не умеет проверять UUID")
		default:
			logf("User UUID check: "+err.Error(), logging.LevelWarn)
			return errors.New("Не удалось связаться с сервером, чтобы проверить UUID")
		}
	}
	store.SetUserUUIDAndOutbounds(uuid, nil)
	store.SetConfigSavedAt(time.Now().Unix())
	if err := store.Save(dataDir); err != nil {
		return errors.New("Не удалось сохранить настройки")
	}
	return nil
}

func setDirectDomains(block string) error {
	list, err := config.ParseAndValidateDirectDomains(block)
	if err != nil {
		return fmt.Errorf("Прямые домены: %w", err)
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	store.SetDirectDomains(list)
	if err := store.Save(dataDir); err != nil {
		return errors.New("Не удалось сохранить прямые домены")
	}
	return nil
}

// setExcludedPackages saves the apps that go around the VPN. They are kept
// where Windows keeps excluded process names, and become the tun inbound's
// exclude_package.
func setExcludedPackages(names []string) error {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	storeMu.Lock()
	defer storeMu.Unlock()
	store.SetIgnoredProcessNames(out)
	store.SetIgnoredProcessPaths(nil)
	return store.Save(dataDir)
}

// ExcludedPackages is the saved list, for the VPN service.
func ExcludedPackages() string {
	if store == nil {
		return ""
	}
	return strings.Join(store.GetIgnoredProcessNames(), "\n")
}

func listLogFiles() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "logs"))
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func logContent(name string) (string, error) {
	if name == "" {
		name = controllerLogFile
	}
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "." || base == ".." || strings.Contains(base, "..") {
		return "", errors.New("invalid log file name")
	}
	// Only the tail: a whole log sent over the bridge and into a text view
	// could take megabytes of memory.
	return logging.ReadTail(logPath(base), logging.ViewerMaxBytes)
}

// The exit-address card, as on the desktop: keep-alive off so no connection
// opened before the tunnel is reused for a question about the new route.
var ipInfoCache = ipinfo.NewCache(5*time.Minute, &http.Client{
	Timeout:   4 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
})
