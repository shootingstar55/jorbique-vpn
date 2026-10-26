package constants

// The names the client shows and the directories it writes under. The build
// scripts spell out the bundle, package and release filenames themselves — a
// constant here that nothing in Go reads is a second copy that drifts.
const (
	// User-visible product name (window title, tray, installers, macOS menu).
	AppDisplayName = "Jorbique VPN Extended"
	// Keep the existing Windows task/registry identity so upgrades refresh the
	// registered command rather than leaving an old executable running at login.
	AutostartRegistrationName = "Jorbique VPN"

	// Optional icon beside the executable (generated from assets/app-icon.png via scripts/branding/render-icons.sh).
	AppIconICO = "icon.ico"

	// Windows %APPDATA% / %TEMP% folder names (no spaces).
	AppDataDirName = "JorbiqueVPN"
	TempDirName    = "JorbiqueVPN"
)

// AppVersion is the release id for bundled data (AppData/temp wipe + reinstall when this changes).
// Not maintained by hand: the build scripts resolve the version from git and
// inject it with -ldflags "-X jorbiquevpn/internal/constants.AppVersion=<x.y.z>".
// The literal below is only what an unflagged `go build`/`go test` sees.
var AppVersion = "0.0.0-dev"

// Application constants
const (
	// InstallVersionFileName is written under DataDir after a successful layout; mismatch => full reset.
	InstallVersionFileName = "jorbique_vpn_install_version.txt"

	// Default route next-hop (virtual peer on the TUN link); TUN address is 172.31.255.1/30 in tun_*.json
	SingboxGatewayIP = "172.31.255.2"
	TunIPRange       = "172.31.255.*"
	ProcessExeName   = "sing-box.exe"
)

// Startup constants
const (
	MaxStartupChecks     = 2
	StartupCheckDelaySec = 1
	// ConnectTunWaitMaxSec: how long to wait for singtun to appear. Detection is
	// native/instant, so this only fully elapses when the adapter genuinely fails to
	// come up; kept short so the auto-retry (up to 2 attempts) stays well under ~10s total.
	ConnectTunWaitMaxSec = 5
)

// ServerHost serves the sing-box manifest, the client profile and telemetry.
// ExtendedUpdatePath (/<channel>/) holds this client's sing-box-extended
// profile and builds. Neither is in the repository: the build scripts link
// them in from SERVER_HOST and SERVER_CHANNEL (scripts/server-ldflags.sh).
var (
	ServerHost         = ""
	ExtendedUpdatePath = ""
)

// The sing-box binary manifest and the client profile. The profile carries no
// user UUID — the client fills in its own — so the link is the same for
// everyone; the server answers it with one of its Reality short_id variants.
var (
	SingboxManifestURL = "https://" + ServerHost + ExtendedUpdatePath + "version.json"
	SingboxClientURL   = "https://" + ServerHost + ExtendedUpdatePath + "singbox_client.json"
)
