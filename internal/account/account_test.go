package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testUUID = "11111111-2222-4333-8444-555555555555"

// fakeServer answers like jorbique-account: one slot, and the refusals with the
// account attached.
func fakeServer(t *testing.T, status int, body string) (*[]*http.Request, func()) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	old := baseURL
	baseURL = srv.URL + "/account/v1/"
	return &seen, func() { baseURL = old; srv.Close() }
}

func TestThisDeviceIDIsStableAndCaseBlind(t *testing.T) {
	id, name, _ := ThisDevice(context.Background())
	if !validID(id) || name == "" {
		t.Fatalf("id %q name %q", id, name)
	}
	again, _, _ := ThisDevice(context.Background())
	if again != id {
		t.Fatalf("the ID must not change between calls: %q %q", id, again)
	}
}

func TestCheckInSendsTheDeviceAndUUID(t *testing.T) {
	seen, done := fakeServer(t, 200, `{"name":"egor","uuid":"`+testUUID+`","expires":"2030-01-01","devices_limit":2,
		"devices":[{"id":"abc12345","name":"Mac","os":"macOS 15","current":true,"source":"sub"}],"current_device":{"id":"abc12345","bound":true}}`)
	defer done()
	profile, err := CheckIn(context.Background(), " "+testUUID+" ", true)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "egor" || *profile.DevicesLimit != 2 || *profile.Expires != "2030-01-01" || !profile.CurrentDevice.Bound {
		t.Fatalf("profile %+v", profile)
	}
	if profile.Devices[0].Source != "sub" {
		t.Fatal("subscription device source was lost")
	}
	r := (*seen)[0]
	if r.Method != http.MethodPost || r.URL.Path != "/account/v1/devices/current" {
		t.Fatalf("request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer "+testUUID || !validID(r.Header.Get("X-Jorbique-Device")) {
		t.Fatalf("headers %v", r.Header)
	}
}

func TestRefusals(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   error
	}{
		{403, `{"error":"limit","account":{"name":"egor","devices":[]}}`, ErrLimit},
		{403, `{"error":"unbound","account":{"name":"egor"}}`, ErrUnbound},
		{404, `{"error":"not_found","account":{"name":"egor"}}`, ErrNotFound},
		{426, `{"error":"outdated"}`, ErrOutdated},
		{401, `{"error":"unauthorized"}`, ErrUnknownUUID},
		{502, `bad gateway`, ErrUnavailable},
		{404, `<html>no account service</html>`, ErrUnavailable},
	} {
		_, done := fakeServer(t, c.status, c.body)
		profile, err := CheckIn(context.Background(), testUUID, false)
		done()
		if !errors.Is(err, c.want) {
			t.Fatalf("%d %s: got %v, want %v", c.status, c.body, err, c.want)
		}
		if (c.want == ErrLimit || c.want == ErrUnbound) && (profile == nil || profile.Name != "egor") {
			t.Fatalf("%v must carry the account, got %+v", c.want, profile)
		}
	}
}

// An outdated client shows the server's message, which says where to download.
func TestOutdatedShowsTheServerMessage(t *testing.T) {
	_, done := fakeServer(t, 426, `{"error":"outdated","message":"Обновите Jorbique VPN: example.com/get"}`)
	defer done()
	err := Gate(context.Background(), testUUID, nil)
	if !errors.Is(err, ErrOutdated) || err.Error() != "Обновите Jorbique VPN: example.com/get" {
		t.Fatalf("outdated: %v", err)
	}
	if !strings.Contains(ErrOutdated.Error(), "private-vpn-client") {
		t.Fatal("the default notice must say where to download")
	}
}

// Gate holds the connect back only for the refusals: anything else, the
// account service missing included, must not keep the user offline.
func TestGate(t *testing.T) {
	for status, body := range map[int]string{
		200: `{"name":"egor"}`, 404: `nope`, 500: `oops`,
	} {
		_, done := fakeServer(t, status, body)
		err := Gate(context.Background(), testUUID, nil)
		done()
		if err != nil {
			t.Fatalf("%d: gate refused: %v", status, err)
		}
	}
	_, done := fakeServer(t, 403, `{"error":"limit"}`)
	defer done()
	if err := Gate(context.Background(), testUUID, nil); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
	if !strings.Contains(ErrLimit.Error(), "Личный кабинет") {
		t.Fatal("the refusal must tell the user where to go")
	}
}

func TestGateRejectsUnknownOrExpiredUUID(t *testing.T) {
	_, done := fakeServer(t, 401, `{"error":"unauthorized"}`)
	defer done()
	if err := Gate(context.Background(), testUUID, nil); !errors.Is(err, ErrUnknownUUID) {
		t.Fatalf("authentication refusal was ignored: %v", err)
	}
}

func TestNothingIsSentWithoutAValidUUIDOrID(t *testing.T) {
	seen, done := fakeServer(t, 200, `{}`)
	defer done()
	if _, err := Get(context.Background(), "not-a-uuid"); !errors.Is(err, ErrUnknownUUID) {
		t.Fatal(err)
	}
	if _, err := Unbind(context.Background(), testUUID, "../me"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if len(*seen) != 0 {
		t.Fatalf("sent %d requests", len(*seen))
	}
}

func TestOpenShowsAFullAccountWithoutAnError(t *testing.T) {
	seen, done := fakeServer(t, 403, `{"error":"limit","account":{"name":"egor","devices_limit":1}}`)
	defer done()
	profile, err := Open(context.Background(), testUUID)
	if err != nil || profile == nil || profile.Name != "egor" {
		t.Fatalf("profile %+v err %v", profile, err)
	}
	if r := (*seen)[0]; r.Method != http.MethodPost || r.URL.Path != "/account/v1/devices/current" {
		t.Fatalf("Open must check this device in, got %s %s", r.Method, r.URL.Path)
	}
}
