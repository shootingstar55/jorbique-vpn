// Package account is the client side of the server's personal account
// (jorbique-account behind /account/v1/): the user's name, UUID and expiry,
// and the devices bound to the UUID under the limit set in users.yml.
//
// The limit is soft. The VPN protocols only see the UUID, so it is this client
// that refuses to connect a device the server did not bind. A device is its
// name and OS, the same name the metrics report: two machines with one name
// and OS are one device, and a renamed machine is a new one.
package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/httpsclient"
	"jorbiquevpn/internal/security"
	"jorbiquevpn/internal/telemetry"
)

// Device is one bound device as the server lists it.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	OS       string `json:"os"`
	BoundAt  string `json:"bound_at"`
	LastSeen string `json:"last_seen"`
	Current  bool   `json:"current"`
}

// Profile is what the account screen shows. Expires and DevicesLimit are nil
// when users.yml sets no date or no limit.
type Profile struct {
	Name          string   `json:"name"`
	UUID          string   `json:"uuid"`
	Expires       *string  `json:"expires"`
	DevicesLimit  *int     `json:"devices_limit"`
	Devices       []Device `json:"devices"`
	CurrentDevice struct {
		ID      string `json:"id"`
		Bound   bool   `json:"bound"`
		Revoked bool   `json:"revoked"`
	} `json:"current_device"`
}

// Errors in the client's own words; the UI is Russian.
var (
	ErrLimit       = errors.New("Достигнут лимит устройств. Откройте «Личный кабинет» и отвяжите одно из устройств.")
	ErrUnbound     = errors.New("Это устройство отвязано. Откройте «Личный кабинет» и привяжите его снова.")
	ErrUnknownUUID = errors.New("UUID не найден на сервере")
	ErrUnavailable = errors.New("Личный кабинет сейчас недоступен. Проверьте интернет и попробуйте ещё раз")
	ErrNotFound    = errors.New("Устройство уже отвязано")
	ErrOutdated    = errors.New("Эта версия Jorbique VPN устарела. Скачайте новую: https://" + constants.ServerHost + "/private-vpn-client")
)

// outdated is ErrOutdated with the server's own wording, which names where to
// download the current client.
type outdated struct{ message string }

func (e outdated) Error() string        { return e.message }
func (e outdated) Is(target error) bool { return target == ErrOutdated }

var baseURL = "https://" + constants.ServerHost + "/account/v1/"

const requestTimeout = 10 * time.Second

// GateTimeout bounds the check before a connect: a slow or absent server must
// not hold the tunnel back.
const GateTimeout = 6 * time.Second

// ThisDevice is this machine's ID, name and OS. The ID is derived from the
// name and the platform, not the OS version, so an OS update keeps the slot.
func ThisDevice(ctx context.Context) (id, name, osName string) {
	name, osName = telemetry.Device(ctx)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Устройство"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(name) + "|" + runtime.GOOS))
	return hex.EncodeToString(sum[:16]), name, osName
}

// Get reads the account as this device sees it.
func Get(ctx context.Context, uuid string) (*Profile, error) {
	return call(ctx, uuid, http.MethodGet, "me", nil)
}

// Open is what the account screen loads: it checks this device in first, so
// a new device is bound as soon as the screen is opened while a slot is free,
// not only on its first connect. A full account or an unbound device is not
// an error here: the screen shows it.
func Open(ctx context.Context, uuid string) (*Profile, error) {
	profile, err := CheckIn(ctx, uuid, false)
	if (errors.Is(err, ErrLimit) || errors.Is(err, ErrUnbound)) && profile != nil {
		return profile, nil
	}
	return profile, err
}

// CheckIn reports this device to the server. A new device is bound while a
// slot is free; bind is the account screen's button, the only way back for a
// device the user unbound. On ErrLimit and ErrUnbound the profile is returned
// too, so the screen can list the devices.
func CheckIn(ctx context.Context, uuid string, bind bool) (*Profile, error) {
	_, name, osName := ThisDevice(ctx)
	return call(ctx, uuid, http.MethodPost, "devices/current", map[string]any{"name": name, "os": osName, "bind": bind})
}

// Unbind removes a device from the account; it may be this one.
func Unbind(ctx context.Context, uuid, deviceID string) (*Profile, error) {
	if !validID(deviceID) {
		return nil, ErrNotFound
	}
	return call(ctx, uuid, http.MethodDelete, "devices/"+deviceID, nil)
}

// Gate is the check before a connect. It returns ErrLimit or ErrUnbound when
// the server refuses this device, ErrOutdated when it refuses this client
// version, and nil for anything else: no network, a
// server without the account service, a slow answer. The tunnel is never
// held back by the account service being away.
func Gate(ctx context.Context, uuid string, logf func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, GateTimeout)
	defer cancel()
	_, err := CheckIn(ctx, uuid, false)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrLimit), errors.Is(err, ErrUnbound), errors.Is(err, ErrOutdated):
		return err
	default:
		if logf != nil {
			logf("Account check skipped: " + err.Error())
		}
		return nil
	}
}

func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func call(ctx context.Context, uuid, method, path string, body any) (*Profile, error) {
	uuid = strings.TrimSpace(uuid)
	if !security.ValidUUID(uuid, false) {
		return nil, ErrUnknownUUID // never send what cannot be a UUID
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	id, _, _ := ThisDevice(ctx)
	req.Header.Set("Authorization", "Bearer "+uuid)
	req.Header.Set("X-Jorbique-Device", id)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpsclient.ForHost(constants.ServerHost, requestTimeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusOK {
		var profile Profile
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return &profile, nil
	}
	var refusal struct {
		Error   string   `json:"error"`
		Message string   `json:"message"`
		Account *Profile `json:"account"`
	}
	_ = json.Unmarshal(data, &refusal)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnknownUUID
	case refusal.Error == "limit":
		return refusal.Account, ErrLimit
	case refusal.Error == "unbound":
		return refusal.Account, ErrUnbound
	case refusal.Error == "outdated" && strings.TrimSpace(refusal.Message) != "":
		return nil, outdated{strings.TrimSpace(refusal.Message)}
	case refusal.Error == "outdated":
		return nil, ErrOutdated
	case refusal.Error == "not_found":
		return refusal.Account, ErrNotFound
	}
	return nil, fmt.Errorf("%w (HTTP %d)", ErrUnavailable, resp.StatusCode)
}
