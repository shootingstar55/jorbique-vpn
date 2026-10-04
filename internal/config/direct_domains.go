package config

import (
	"fmt"
	"strings"
)

// ParseAndValidateDirectDomains splits a block into lines, skips empty lines and # comments,
// normalizes hostnames, and validates them for use as sing-box domain_suffix (no regex).
func ParseAndValidateDirectDomains(block string) ([]string, error) {
	lines := strings.Split(block, "\n")
	seen := map[string]struct{}{}
	var out []string
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		norm, err := normalizeDirectDomain(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		key := strings.ToLower(norm)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, norm)
	}
	return out, nil
}

func normalizeDirectDomain(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "", fmt.Errorf("empty domain")
	}
	// Optional *.example.com — suffix rule already matches subdomains
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "*.") {
		s = s[2:]
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, ".")
		low = strings.ToLower(s)
	}
	if s == "" {
		return "", fmt.Errorf("empty domain")
	}
	if len(s) > 253 {
		return "", fmt.Errorf("domain too long")
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, " \t/\\:*?\"<>|") {
		return "", fmt.Errorf("invalid characters in %q (enter a hostname like example.com or office.lan)", s)
	}
	labels := strings.Split(low, ".")
	for _, lab := range labels {
		if err := checkDirectDomainLabel(lab, s); err != nil {
			return "", err
		}
	}
	return low, nil
}

// checkDirectDomainLabel validates one lowercased label of domain s; errors name s.
func checkDirectDomainLabel(lab, s string) error {
	if lab == "" {
		return fmt.Errorf("empty label in %q", s)
	}
	if len(lab) > 63 {
		return fmt.Errorf("label too long in %q", s)
	}
	for _, r := range lab {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("use only letters, digits and hyphens in %q (for IDN TLDs, paste punycode such as xn--...)", s)
	}
	if lab[0] == '-' || lab[len(lab)-1] == '-' {
		return fmt.Errorf("invalid label in %q", s)
	}
	return nil
}
