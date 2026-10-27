package ipinfo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The three services answer the same question in three shapes. These are their
// real ones, trimmed.
const (
	ipwhoBody = `{"ip":"104.28.219.140","success":true,"country":"Czechia","country_code":"CZ",
	  "region":"Prague","city":"Prague","latitude":50.09,"longitude":14.42,
	  "timezone":{"id":"Europe/Prague"},
	  "connection":{"asn":29208,"isp":"Quantcom, a.s.","org":"Quantcom, a.s."}}`
	geojsBody = `{"ip":"104.28.219.140","country":"Czechia","country_code":"CZ","city":"Prague",
	  "region":"Prague","organization_name":"Quantcom, a.s.","asn":29208,
	  "timezone":"Europe/Prague","latitude":"50.09","longitude":"14.42"}`
)

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// Whichever source answers, the card gets the same fields — that is the whole
// point of normalising them here rather than in two front-ends.
func TestEverySourceShapeNormalisesToTheSameAnswer(t *testing.T) {
	for name, body := range map[string]string{"ipwho.is": ipwhoBody, "geojs": geojsBody} {
		t.Run(name, func(t *testing.T) {
			info, err := Fetch(context.Background(), nil, []string{serve(t, 200, body)})
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if info.IP != "104.28.219.140" || info.CountryCode != "CZ" || info.City != "Prague" {
				t.Errorf("address/place = %+v", info)
			}
			if info.ASN != "AS29208" {
				t.Errorf("asn = %q, want AS29208 — the number, however the source spelled it", info.ASN)
			}
			if info.ISP == "" || info.Timezone != "Europe/Prague" {
				t.Errorf("provider/timezone = %+v", info)
			}
			if info.Latitude < 50 || info.Latitude > 51 || info.Longitude < 14 || info.Longitude > 15 {
				t.Errorf("coordinates = %v, %v", info.Latitude, info.Longitude)
			}
		})
	}
}

// A source that is down, rate-limited, or answering with something that is not
// an address must not stop the next one from being asked.
func TestFetchFallsThroughToASourceThatWorks(t *testing.T) {
	down := serve(t, 503, "service unavailable")
	refused := serve(t, 200, `{"success":false,"message":"rate limited"}`)
	failedLookup := serve(t, 200, `{"message":"private range"}`)
	good := serve(t, 200, ipwhoBody)

	info, err := Fetch(context.Background(), nil, []string{down, refused, failedLookup, good})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if info.IP != "104.28.219.140" {
		t.Errorf("ip = %q", info.IP)
	}

	if _, err := Fetch(context.Background(), nil, []string{down, refused}); err == nil {
		t.Error("no error when every source failed")
	}
}

// The card polls; the services rate-limit. One request per ttl, and the last
// good answer stays on screen when a refresh fails.
func TestCacheAsksOncePerTTLAndKeepsTheLastGoodAnswer(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte(ipwhoBody))
	}))
	t.Cleanup(server.Close)

	previous := Sources
	Sources = []string{server.URL}
	t.Cleanup(func() { Sources = previous })

	cache := NewCache(time.Minute, nil)
	if _, _, ok := cache.Peek(); ok {
		t.Error("Peek answered before anything was fetched")
	}
	if _, err := cache.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	// Peek reads what Get stored and never asks the service itself.
	if info, fetched, ok := cache.Peek(); !ok || info.IP != "104.28.219.140" || calls != 1 {
		t.Errorf("Peek = %+v, %v after %d calls", info, ok, calls)
	} else if !fetched.Equal(cache.fetched) {
		t.Errorf("Peek's time %v is not the one the ttl runs on, %v", fetched, cache.fetched)
	}
	if _, err := cache.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("asked %d times inside the ttl, want 1", calls)
	}

	// A refresh the user pressed says the lookup failed, rather than handing back
	// the value they pressed the button to change.
	if _, err := cache.Get(context.Background(), true); err == nil {
		t.Error("a forced refresh that failed reported success")
	}
	if calls != 2 {
		t.Errorf("force did not ask again (%d calls)", calls)
	}

	// One that ran on its own keeps the card as it was.
	cache.fetched = time.Now().Add(-2 * time.Minute)
	info, err := cache.Get(context.Background(), false)
	if err != nil {
		t.Fatalf("an automatic refresh became an error: %v", err)
	}
	if info.IP != "104.28.219.140" {
		t.Errorf("the last good answer was lost: %+v", info)
	}
}

// ipinfo.io is the odd one: the country field holds the two-letter code, the
// provider and its AS number share one string, and the coordinates arrive as a
// single "lat,lon".
func TestIpinfoIoShapeIsUnpacked(t *testing.T) {
	const body = `{"ip":"193.179.119.66","city":"Ostrava","region":"Moravian-Silesian Region",
	  "country":"CZ","loc":"49.8347,18.2820","org":"AS13036 T-Mobile Czech Republic a.s.",
	  "timezone":"Europe/Prague"}`

	info, err := Fetch(context.Background(), nil, []string{serve(t, 200, body)})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if info.CountryCode != "CZ" || info.Country != "" {
		t.Errorf("country = %q / %q, want the code alone", info.Country, info.CountryCode)
	}
	if info.ASN != "AS13036" {
		t.Errorf("asn = %q", info.ASN)
	}
	if info.Org == "" {
		t.Errorf("org = %q, want the provider", info.Org)
	}
	if info.Latitude < 49 || info.Latitude > 50 || info.Longitude < 18 || info.Longitude > 19 {
		t.Errorf("coordinates = %v, %v", info.Latitude, info.Longitude)
	}
}

// Every lookup has to open its own connection. A pooled one survives the tunnel
// coming up and keeps leaving by the path it was opened on, which is how a
// connected client kept reporting the address it had before the connect.
func TestLookupsDoNotReuseAConnection(t *testing.T) {
	connections := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ipwhoBody))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections++
		}
	}
	t.Cleanup(server.Close)

	for i := 0; i < 3; i++ {
		if _, err := Fetch(context.Background(), nil, []string{server.URL}); err != nil {
			t.Fatalf("Fetch %d: %v", i, err)
		}
	}
	if connections != 3 {
		t.Errorf("3 lookups opened %d connections; they are being pooled", connections)
	}
}

// blockingServer answers ipwhoBody once release closes, and counts requests.
// A request whose client went away returns at once, so Close never hangs.
func blockingServer(t *testing.T) (release chan struct{}, hits *atomic.Int32) {
	t.Helper()
	release, hits = make(chan struct{}), &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
			_, _ = w.Write([]byte(ipwhoBody))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	previous := Sources
	Sources = []string{server.URL}
	t.Cleanup(func() { Sources = previous })
	return release, hits
}

// The menu, the window card and a settle asking at once send one request
// between them, and each gets its answer.
func TestCacheSharesOneLookupBetweenCallers(t *testing.T) {
	release, hits := blockingServer(t)
	cache := NewCache(time.Minute, nil)
	answers := make(chan string, 3)
	for _, force := range []bool{false, true, true} {
		go func() {
			info, err := cache.Get(context.Background(), force)
			if err != nil {
				t.Error(err)
			}
			answers <- info.IP
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	for range 3 {
		if ip := <-answers; ip != "104.28.219.140" {
			t.Errorf("a caller got %q", ip)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("3 callers at once sent %d requests, want 1", n)
	}
}

// A lookup asked before the route changed answers for the old route: the
// change cancels it and nothing of it is kept. Its caller is not handed the
// cancellation — the window's card showed it as an error — but asked again,
// once: a second change during that lookup is passed on rather than chased.
func TestInvalidateCancelsTheLookupInFlight(t *testing.T) {
	release, hits := blockingServer(t)
	cache := NewCache(time.Minute, nil)
	type answer struct {
		info Info
		err  error
	}
	ask := func() <-chan answer {
		out := make(chan answer, 1)
		go func() {
			info, err := cache.Get(context.Background(), true)
			out <- answer{info, err}
		}()
		return out
	}
	requests := func(n int32) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); hits.Load() < n; time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%d requests sent, want %d", hits.Load(), n)
			}
		}
	}

	twice := ask()
	requests(1)
	cache.Invalidate()
	requests(2) // asked again for the new route
	cache.Invalidate()
	if got := <-twice; !errors.Is(got.err, ErrRouteChanged) {
		t.Fatalf("a lookup the route changed under twice answered %+v, %v; want ErrRouteChanged", got.info, got.err)
	}

	once := ask()
	requests(3)
	cache.Invalidate()
	requests(4)
	if info, _, ok := cache.Peek(); ok {
		t.Errorf("the old route's answer was kept: %+v", info)
	}
	close(release)
	if got := <-once; got.err != nil || got.info.IP != "104.28.219.140" {
		t.Fatalf("Get across a route change = %+v, %v; want the new route's answer", got.info, got.err)
	}
	if n := hits.Load(); n != 4 {
		t.Errorf("sent %d requests, want 4: two per Get, the one a change cancelled and its single retry", n)
	}
}
