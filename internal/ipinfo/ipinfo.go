// Package ipinfo answers "where does my traffic come out" in full: the exit
// address and everything a user checks it against — country, city, provider,
// network. Both clients show the same card from it.
//
// It replaces two half-answers: the Windows client asked geojs for an address
// and a country to render as one line, and the macOS menu bar asked its own
// chain of services for the same pair. Neither could say which provider the
// traffic appeared to come from, which is the question a VPN user is actually
// asking.
package ipinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Info is one provider's answer, normalised.
type Info struct {
	IP          string  `json:"ip"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	City        string  `json:"city"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	ASN         string  `json:"asn"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	// FetchedAt is when this answer came back, so the card can say how old it is.
	FetchedAt int64 `json:"fetchedAt"`
}

// Sources are tried in order until one answers with a usable address.
//
// All three are plain HTTPS with no key. ip-api.com — the one most of these
// clients reach for first — is not here: its free tier answers 403 over HTTPS
// and only serves plain HTTP, and an exit address is not worth sending a
// request in the clear for.
var Sources = []string{
	"https://ipwho.is/",
	"https://get.geojs.io/v1/ip/geo.json",
	"https://ipinfo.io/json",
}

const requestTimeout = 8 * time.Second

// newClient builds the client used when a caller passes none.
//
// Keep-alive is off, and that is the whole point: a pooled connection opened
// before the tunnel came up stays on the path it was opened on, so every lookup
// reusing it answers about the old route. That is exactly what "connected, but
// the card still shows my home address" was — the Windows client already passes
// a no-keep-alive client for the same reason, and the default has to match it
// rather than leave the macOS side to find out the hard way.
//
// No proxy either: this asks where the machine's traffic comes out, and a proxy
// from the environment would answer about itself.
func newClient() *http.Client {
	return &http.Client{
		Timeout:   requestTimeout,
		Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil},
	}
}

// Fetch asks each source in turn and returns the first usable answer.
func Fetch(ctx context.Context, client *http.Client, sources []string) (Info, error) {
	if client == nil {
		client = newClient()
	}
	var lastErr error
	for _, source := range sources {
		info, err := fetchOne(ctx, client, source)
		if err != nil {
			lastErr = err
			continue
		}
		return info, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no source answered")
	}
	return Info{}, lastErr
}

func fetchOne(ctx context.Context, client *http.Client, source string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return Info{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Info{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("%s: %s", source, response.Status)
	}

	// One struct over three shapes. Each field is named once: a duplicated JSON
	// tag makes encoding/json drop both fields silently, which is what an
	// earlier version did to the country, the city and the timezone.
	//
	// Where the same name carries different types — ipwho.is answers an object
	// for "timezone" and a number for "latitude", geojs a string for both — the
	// value is kept raw and read below.
	var body struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
		City    string `json:"city"`
		Region  string `json:"region"`

		// ipwho.is
		Success    *bool           `json:"success"`
		CountryISO string          `json:"country_code"`
		Latitude   json.RawMessage `json:"latitude"`
		Longitude  json.RawMessage `json:"longitude"`
		Timezone   json.RawMessage `json:"timezone"`
		Connection struct {
			ASN int    `json:"asn"`
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`

		// geojs
		GeoOrgName string `json:"organization_name"`
		GeoASN     int    `json:"asn"`

		// ipinfo.io: "org" is "AS13036 T-Mobile Czech Republic a.s." and the
		// coordinates come as one "lat,lon" string.
		Org string `json:"org"`
		Loc string `json:"loc"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return Info{}, fmt.Errorf("%s: %w", source, err)
	}
	if body.Success != nil && !*body.Success {
		return Info{}, fmt.Errorf("%s: refused", source)
	}
	// ipinfo.io answers the code in the same field the others use for the name.
	country, countryCode := body.Country, body.CountryISO
	if countryCode == "" && len(country) == 2 {
		country, countryCode = "", country
	}
	latitude, longitude := numberOf(body.Latitude), numberOf(body.Longitude)
	if latitude == 0 && longitude == 0 {
		latitude, longitude = parsePair(body.Loc)
	}

	info := Info{
		IP:          body.IP,
		Country:     country,
		CountryCode: strings.ToUpper(countryCode),
		Region:      body.Region,
		City:        body.City,
		Timezone:    timezoneOf(body.Timezone),
		ISP:         firstNonEmpty(body.Connection.ISP, body.GeoOrgName),
		Org:         firstNonEmpty(body.Connection.Org, body.Org, body.GeoOrgName),
		Latitude:    latitude,
		Longitude:   longitude,
		FetchedAt:   time.Now().Unix(),
	}
	info.ASN = normalizeASN(body.Connection.ASN, body.Org, body.GeoASN)
	if net.ParseIP(strings.TrimSpace(info.IP)) == nil {
		return Info{}, fmt.Errorf("%s: no usable address", source)
	}
	return info, nil
}

// normalizeASN renders whichever form the source used as "AS29208": a bare
// number from ipwho.is and geojs, and "AS13036 T-Mobile …" from ipinfo.io.
func normalizeASN(number int, text string, fallback int) string {
	if number > 0 {
		return fmt.Sprintf("AS%d", number)
	}
	if fields := strings.Fields(text); len(fields) > 0 && strings.HasPrefix(fields[0], "AS") {
		return fields[0]
	}
	if fallback > 0 {
		return fmt.Sprintf("AS%d", fallback)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

// timezoneOf reads a "timezone" that is an object with an id (ipwho.is) or the
// name itself (geojs, ipinfo.io).
func timezoneOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && object.ID != "" {
		return object.ID
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return ""
}

// parsePair reads ipinfo.io's "50.07,14.39".
func parsePair(value string) (float64, float64) {
	parts := strings.SplitN(strings.TrimSpace(value), ",", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	var latitude, longitude float64
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%g", &latitude); err != nil {
		return 0, 0
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%g", &longitude); err != nil {
		return 0, 0
	}
	return latitude, longitude
}

// numberOf reads a coordinate that is a number (ipwho.is) or a quoted one (geojs).
func numberOf(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(text), "%g", &number); err != nil {
		return 0
	}
	return number
}

// Cache holds the last answer. The card refreshes every few minutes and both
// clients poll their status far more often than that, so without this every
// poll would be a request to a public service.
//
// One lookup at a time: a Get that finds one in flight waits for it rather
// than sending its own — the menu's, the window card's and a settle after a
// connect would otherwise each ask the same rate-limited services at once. A
// route change (Invalidate) cancels the one in flight instead of leaving it to
// run: it left by the old path, so nobody may wait on it, keep its answer, or
// race the next one with it.
type Cache struct {
	mu      sync.Mutex
	info    Info
	fetched time.Time
	flight  *lookup // the lookup in flight, nil when none
	ttl     time.Duration
	client  *http.Client
}

// lookup is one Fetch every Get asking while it runs shares. info and err are
// set before done closes.
type lookup struct {
	done   chan struct{}
	cancel context.CancelFunc
	info   Info
	err    error
}

// ErrRouteChanged is what a lookup the route changed under answers: the
// address it was asking about is not the one traffic leaves by any more. Get
// passes it on only when the route changes again under the lookup it asks in
// its place.
var ErrRouteChanged = errors.New("the route changed while the exit address was being asked; ask again")

// NewCache returns a cache that re-asks at most once per ttl.
func NewCache(ttl time.Duration, client *http.Client) *Cache {
	return &Cache{ttl: ttl, client: client}
}

// Get returns the cached answer, asking again when it is older than the ttl or
// when force says the user pressed refresh. A forced Get joins a lookup already
// in flight too: that one started after the last route change, so it answers
// the same question.
//
// A lookup the route changed under is asked again once rather than passed on:
// the caller wants to know where traffic comes out now, and the window's card
// would show ErrRouteChanged as if the service had refused. Once, not until it
// sticks: a second change during the second lookup means the route is still
// moving, and every caller asks again on its own after a change anyway.
func (c *Cache) Get(ctx context.Context, force bool) (Info, error) {
	info, err := c.get(ctx, force)
	if errors.Is(err, ErrRouteChanged) {
		info, err = c.get(ctx, force)
	}
	return info, err
}

// get is one round of Get.
func (c *Cache) get(ctx context.Context, force bool) (Info, error) {
	c.mu.Lock()
	if !force && c.info.IP != "" && time.Since(c.fetched) < c.ttl {
		info := c.info
		c.mu.Unlock()
		return info, nil
	}
	if c.flight == nil {
		c.flight = c.start()
	}
	l := c.flight
	c.mu.Unlock()

	select {
	case <-l.done:
	case <-ctx.Done():
		return Info{}, ctx.Err()
	}
	if l.err != nil {
		// A refresh that ran on its own keeps the last good answer rather than
		// blanking the card: the address has not changed just because a public
		// service is having a moment. One the user asked for says so instead —
		// answering with the same value they pressed the button to change looks
		// exactly like a button that does nothing.
		if force {
			return Info{}, l.err
		}
		c.mu.Lock()
		last := c.info
		c.mu.Unlock()
		if last.IP != "" {
			return last, nil
		}
		return Info{}, l.err
	}
	return l.info, nil
}

// start sends the lookup the Gets until it lands share. Its own context, not
// a caller's: one caller giving up must not fail the others. Called with mu
// held.
func (c *Cache) start() *lookup {
	ctx, cancel := context.WithCancel(context.Background())
	l := &lookup{done: make(chan struct{}), cancel: cancel}
	client := c.client
	go func() {
		defer cancel()
		info, err := Fetch(ctx, client, Sources)
		c.mu.Lock()
		if c.flight == l {
			c.flight = nil
			if err == nil {
				c.info, c.fetched = info, time.Now()
			}
		} else {
			// Invalidate let go of it: whatever came back is the old route's.
			info, err = Info{}, ErrRouteChanged
		}
		c.mu.Unlock()
		l.info, l.err = info, err
		close(l.done)
	}()
	return l
}

// Invalidate drops the cached answer and cancels the lookup in flight, so the
// next Get asks again — the exit address is a different one after a connect or
// a disconnect.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.info, c.fetched = Info{}, time.Time{}
	if c.flight != nil {
		c.flight.cancel()
		c.flight = nil
	}
	c.mu.Unlock()
}

// Peek returns the cached answer without asking anyone, stale or not, when it
// was stored, and whether there is one. For a caller that must never wait on a
// lookup — the menu bar reads it on every refresh, on the main thread. A caller
// judging its age goes by fetched, the clock Get's ttl runs on: FetchedAt is
// whole seconds and can make an answer Get still holds look expired.
func (c *Cache) Peek() (info Info, fetched time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info, c.fetched, c.info.IP != ""
}
