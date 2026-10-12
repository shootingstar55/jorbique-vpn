package vless

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"jorbiquevpn/internal/security"
)

var ErrInvalidURL = errors.New("invalid URL")

var (
	vlessURLRe  = regexp.MustCompile(`^vless://([^@]+)@([^?#]+)\??(.*)$`)
	anyTLSURLRe = regexp.MustCompile(`^anytls://([^@]+)@([^?#]+)\??(.*)$`)
)

type ParsedGrpc struct {
	Server      string
	ServerPort  int
	UUID        string
	ServiceName string
	Security    string
	ServerName  string
}

type ParsedAnyTLS struct {
	Server     string
	ServerPort int
	Password   string
	ServerName string
}

// ParseVlessGrpc parses vless gRPC URL
func ParseVlessGrpc(vlessURL string) (*ParsedGrpc, error) {
	vlessURL = strings.TrimSpace(vlessURL)
	if idx := strings.Index(vlessURL, "#"); idx >= 0 {
		vlessURL = vlessURL[:idx]
	}
	if !strings.HasPrefix(vlessURL, "vless://") {
		return nil, ErrInvalidURL
	}
	m := vlessURLRe.FindStringSubmatch(vlessURL)
	if m == nil {
		return nil, ErrInvalidURL
	}
	uuid := strings.TrimSpace(m[1])
	hostPort := strings.TrimSpace(m[2])
	query := m[3]

	server, port, err := parseHostPort(hostPort)
	if err != nil {
		return nil, err
	}
	if !security.ValidUUID(uuid, false) {
		return nil, ErrInvalidURL
	}

	params := parseQuery(query)
	serviceName := params["servicename"]
	if serviceName == "" {
		serviceName = "MediaService"
	}
	sec := params["security"]
	sni := params["sni"]

	result := &ParsedGrpc{
		Server:      server,
		ServerPort:  port,
		UUID:        uuid,
		ServiceName: serviceName,
	}
	if sec == "tls" {
		result.Security = "tls"
		if sni != "" {
			result.ServerName = sni
		} else {
			result.ServerName = server
		}
	}
	return result, nil
}

func serverName(server, sni string) string {
	if sni != "" {
		return sni
	}
	return server
}

// ParseAnyTLS parses anytls://uuid@host:port?sni=... (userinfo is the password, often a UUID from subscription links).
func ParseAnyTLS(anytlsURL string) (*ParsedAnyTLS, error) {
	anytlsURL = strings.TrimSpace(anytlsURL)
	if idx := strings.Index(anytlsURL, "#"); idx >= 0 {
		anytlsURL = anytlsURL[:idx]
	}
	if !strings.HasPrefix(anytlsURL, "anytls://") {
		return nil, ErrInvalidURL
	}
	m := anyTLSURLRe.FindStringSubmatch(anytlsURL)
	if m == nil {
		return nil, ErrInvalidURL
	}
	password := strings.TrimSpace(m[1])
	if u, err := url.QueryUnescape(password); err == nil {
		password = u
	}
	hostPort := strings.TrimSpace(m[2])
	query := m[3]
	if strings.TrimSpace(password) == "" {
		return nil, ErrInvalidURL
	}
	server, port, err := parseHostPort(hostPort)
	if err != nil {
		return nil, err
	}
	params := parseQuery(query)
	sni := params["sni"]
	return &ParsedAnyTLS{
		Server:     server,
		ServerPort: port,
		Password:   password,
		ServerName: serverName(server, sni),
	}, nil
}

func parseHostPort(hostPort string) (string, int, error) {
	hostPort = strings.TrimSpace(hostPort)
	if strings.HasPrefix(hostPort, "[") {
		idx := strings.Index(hostPort, "]:")
		if idx < 0 {
			return "", 0, ErrInvalidURL
		}
		server := hostPort[1:idx]
		port, err := strconv.Atoi(hostPort[idx+2:])
		if err != nil || port <= 0 {
			return "", 0, ErrInvalidURL
		}
		return server, port, nil
	}
	idx := strings.LastIndex(hostPort, ":")
	if idx < 0 {
		return "", 0, ErrInvalidURL
	}
	server := hostPort[:idx]
	port, err := strconv.Atoi(hostPort[idx+1:])
	if err != nil || port <= 0 {
		return "", 0, ErrInvalidURL
	}
	return server, port, nil
}

func parseQuery(query string) map[string]string {
	params := make(map[string]string)
	for _, pair := range strings.Split(query, "&") {
		idx := strings.Index(pair, "=")
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(pair[:idx]))
		v, _ := url.QueryUnescape(strings.TrimSpace(pair[idx+1:]))
		params[k] = v
	}
	return params
}
