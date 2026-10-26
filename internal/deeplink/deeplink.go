// Package deeplink reads the links the /sub page opens the client with:
//
//	jorbique://import?uuid=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
//
// The page knows the user's UUID from its own address, so one click hands it to
// the installed client instead of the user copying it across by hand. Every
// platform registers the scheme its own way (the Windows registry, the macOS
// bundle's CFBundleURLTypes, the Android manifest); this is the one parser they
// share on the Go side, and it accepts nothing but a well-formed UUID.
package deeplink

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"jorbiquevpn/internal/security"
)

// Scheme is the URL scheme the client registers.
const Scheme = "jorbique"

// ImportUUID returns the UUID an import link carries, lower-cased. ok is false
// for anything else: another scheme, another action, a missing or malformed
// UUID. A link is something any web page can open, so nothing beyond a UUID is
// ever read from it.
func ImportUUID(raw string) (uuid string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, Scheme) {
		return "", false
	}
	// jorbique://import?… parses with the action as the host; jorbique:import?…
	// and jorbique:///import?… put it in the opaque part or the path instead.
	action := u.Host
	if action == "" {
		action = strings.Trim(u.Opaque+u.Path, "/")
	}
	if !strings.EqualFold(strings.Trim(action, "/"), "import") {
		return "", false
	}
	uuid = strings.ToLower(strings.TrimSpace(u.Query().Get("uuid")))
	if !security.ValidUUID(uuid, false) {
		return "", false
	}
	return uuid, true
}

// FromArgs returns the UUID of the first import link among command-line
// arguments: Windows starts the client with the link as an argument.
func FromArgs(args []string) (uuid string, ok bool) {
	for _, arg := range args {
		if uuid, ok := ImportUUID(arg); ok {
			return uuid, true
		}
	}
	return "", false
}

// HasLink reports whether any argument is a link of this scheme, valid or not,
// so a malformed one is not mistaken for a normal start.
func HasLink(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), Scheme+":") {
			return true
		}
	}
	return false
}

// pendingFile is where a link waits for the window to pick it up.
const pendingFile = "pending_import_uuid"

// SavePending leaves uuid in dataDir for the running app (or the one about to
// start) to take.
func SavePending(dataDir, uuid string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, pendingFile), []byte(uuid+"\n"), 0o600)
}

// HasPending reports whether a link is waiting, without taking it.
func HasPending(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, pendingFile))
	return err == nil
}

// TakePending returns the waiting UUID and removes it, so it is handled once.
// Empty when there is none, or when what is there is not a UUID.
func TakePending(dataDir string) string {
	path := filepath.Join(dataDir, pendingFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	uuid := strings.ToLower(strings.TrimSpace(string(data)))
	if !security.ValidUUID(uuid, false) {
		return ""
	}
	return uuid
}
