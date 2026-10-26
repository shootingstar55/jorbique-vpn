package autoconnect

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/singboxcfg"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/utils"
)

const (
	PayloadBytes = 64 * 1024
	ProbeTimeout = 10 * time.Second
	ScanTimeout  = 75 * time.Second
	probeURL     = "https://speed.cloudflare.com/__down?bytes=65536"
)

// Runner owns private scratch files and a differently named engine executable.
// Windows' tunnel recovery scans for sing-box.exe: a probe must never be
// mistaken for an established tunnel. The Cronet loader needs its DLL beside
// that executable as well as the same working directory as the real engine.
type Runner struct {
	dir, binary, workingDir string
	profile                 []byte
	uuid                    string
	target                  string
	tlsTransport            *http.Transport
}

func NewRunner(binary, workingDir string, profile []byte, uuid string) (*Runner, error) {
	dir, err := os.MkdirTemp("", "jorbique-connection-check-")
	if err != nil {
		return nil, err
	}
	r := &Runner{dir: dir, workingDir: workingDir, profile: profile, uuid: uuid}
	name := "jorbique-connection-check"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	r.binary = filepath.Join(dir, name)
	if err := linkOrCopy(binary, r.binary); err != nil {
		r.Close()
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Dir(binary))
	if err != nil {
		r.Close()
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(strings.ToLower(name), "libcronet.") {
			continue
		}
		if err := linkOrCopy(filepath.Join(filepath.Dir(binary), name), filepath.Join(dir, name)); err != nil {
			r.Close()
			return nil, err
		}
	}
	return r, nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (r *Runner) Close() { _ = os.RemoveAll(r.dir) }

func (r *Runner) Probe(ctx context.Context, tag string) (Measurement, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Measurement{}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	// Binding this port is not a health check. Only the authenticated SOCKS
	// transfer below counts; a different local service cannot fake a success.
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Measurement{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	root, err := ProbeConfig(r.profile, r.uuid, tag, port, token)
	if err != nil {
		return Measurement{}, err
	}
	body, err := jsonorder.MarshalIndent(root)
	if err != nil {
		return Measurement{}, err
	}
	path := filepath.Join(r.dir, "probe.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		return Measurement{}, err
	}
	defer os.Remove(path)
	cmd := sysexec.CommandContext(ctx, r.binary, "run", "-c", path)
	cmd.Dir = r.workingDir
	cmd.Env = singboxcfg.EngineEnvironment(os.Environ())
	utils.HideCommandWindow(cmd)
	// Do not forward engine errors: URLs and server credentials can appear in
	// them. The caller reports only a protocol's availability and measurements.
	if err := cmd.Start(); err != nil {
		return Measurement{}, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	for {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		select {
		case <-ctx.Done():
			return Measurement{}, ctx.Err()
		case <-done:
			return Measurement{}, fmt.Errorf("connection check did not start")
		case <-time.After(30 * time.Millisecond):
		}
	}
	target := r.target
	if target == "" {
		target = probeURL
	}
	return measure(ctx, address, token, target, PayloadBytes, r.tlsTransport)
}

// ProbeConfig contains no TUN, API listener, route rules, disk cache, or remote
// rule-set downloads. The requested leaf is the final route, never direct.
// Credential application uses the same exact-field contract as both clients.
func ProbeConfig(profile []byte, uuid, tag string, port int, token string) (*jsonorder.Map, error) {
	root := jsonorder.New()
	if err := jsonorder.Unmarshal(profile, root); err != nil {
		return nil, err
	}
	available := Candidates(root)
	enabled := map[string]bool{}
	found := false
	for _, candidate := range available {
		enabled[candidate] = candidate == tag
		found = found || candidate == tag
	}
	if !found {
		return nil, fmt.Errorf("protocol is not offered by the server")
	}
	raw, _ := root.Get("outbounds")
	obs, _ := raw.([]any)
	filtered, err := singboxcfg.FilterOutbounds(obs, root, enabled)
	if err != nil {
		return nil, err
	}
	for _, raw := range filtered {
		ob, ok := raw.(*jsonorder.Map)
		if ok && ob.GetString("tag") == tag {
			singboxcfg.ApplyCredential(ob, uuid)
		}
	}
	root.Set("outbounds", filtered)
	for _, key := range []string{"services", "experimental", "endpoints", "ntp"} {
		root.Delete(key)
	}
	root.Set("log", jsonorder.FromMap(map[string]any{"disabled": true}))
	root.Set("inbounds", []any{jsonorder.FromMap(map[string]any{
		"type": "socks", "tag": "connection-check", "listen": "127.0.0.1", "listen_port": port,
		"users": []any{map[string]any{"username": "check", "password": token}},
	})})
	routeRaw, _ := root.Get("route")
	route, _ := routeRaw.(*jsonorder.Map)
	if route == nil {
		route = jsonorder.New()
		root.Set("route", route)
	}
	route.Set("rules", []any{})
	route.Delete("rule_set")
	route.Set("final", tag)
	route.Set("auto_detect_interface", true)
	singboxcfg.PruneDanglingReferences(root)
	pinGroups(root, tag)
	singboxcfg.SortRoot(root)
	return root, nil
}

// measure deliberately uses SOCKS remote DNS, trusted HTTPS, an exact payload
// length, and two transfers. An open port, redirect, HTTP challenge, short
// body, or 16-KiB stall cannot qualify as a working connection.
func measure(ctx context.Context, address, token, target string, size int64, tlsTransport *http.Transport) (Measurement, error) {
	dialer, err := proxy.SOCKS5("tcp", address, &proxy.Auth{User: "check", Password: token}, &net.Dialer{Timeout: time.Second})
	if err != nil {
		return Measurement{}, err
	}
	transport := &http.Transport{DisableCompression: true, ForceAttemptHTTP2: true}
	if tlsTransport != nil {
		transport = tlsTransport.Clone()
	}
	transport.DisableCompression = true
	transport.Proxy = nil
	transport.DialContext = dialer.(proxy.ContextDialer).DialContext
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	result := Measurement{}
	for range 2 {
		start := time.Now()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return Measurement{}, err
		}
		request.Header.Set("Cache-Control", "no-cache")
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{GotFirstResponseByte: func() { result.Latency += time.Since(start) }}))
		response, err := client.Do(request)
		if err != nil {
			return Measurement{}, err
		}
		n, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, size+1))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || n != size {
			return Measurement{}, fmt.Errorf("HTTPS transfer incomplete")
		}
		result.Elapsed += time.Since(start)
		result.Bytes += n
	}
	result.Latency /= 2
	return result, nil
}
