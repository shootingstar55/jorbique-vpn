package core

import (
	"os"
)

// StripTrailingCommas removes JSONC-style trailing commas before `}` or `]`.
// Swift's JSONSerialization is lenient about these; Go's encoding/json is strict.
func StripTrailingCommas(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inStr := false
	escaped := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inStr {
			out = append(out, c)
			inStr, escaped = stepInString(c, escaped)
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' && closesAfterWhitespace(in, i+1) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// stepInString advances the string-literal state past c and returns whether
// the string is still open and whether the next byte is escaped.
func stepInString(c byte, escaped bool) (inStr, nextEscaped bool) {
	if escaped {
		return true, false
	}
	switch c {
	case '\\':
		return true, true
	case '"':
		return false, false
	}
	return true, false
}

// closesAfterWhitespace reports whether the first byte from j on that is not
// JSON whitespace is `}` or `]`.
func closesAfterWhitespace(in []byte, j int) bool {
	for j < len(in) && (in[j] == ' ' || in[j] == '\t' || in[j] == '\n' || in[j] == '\r') {
		j++
	}
	return j < len(in) && (in[j] == '}' || in[j] == ']')
}

// ReadConfig overlays user settings on the single managed sing-box core.
func ReadConfig(paths *Paths) (*AppConfig, error) {
	cfg := BuiltinConfig()
	if data, err := os.ReadFile(paths.CfgFile); err == nil {
		applyOverlay(cfg, StripTrailingCommas(data))
	} else if !os.IsNotExist(err) {
		return nil, BadConfig(err.Error())
	}

	// Resolve relative paths to absolute.
	resolvedVPNs := make([]VPNItem, 0, len(cfg.VPNs))
	for _, v := range cfg.VPNs {
		v.File = paths.AbsolutePathForConfigField(v.File)
		resolvedVPNs = append(resolvedVPNs, v)
	}
	cfg.VPNs = resolvedVPNs

	if cfg.StartedListPath != nil {
		s := *cfg.StartedListPath
		if s != "" {
			abs := paths.AbsolutePathForConfigField(s)
			cfg.StartedListPath = &abs
		} else {
			cfg.StartedListPath = nil
		}
	}

	cfg.DirectBypassProcesses = filterNonEmpty(cfg.DirectBypassProcesses)
	cfg.DirectBypassPaths = filterNonEmpty(cfg.DirectBypassPaths)
	cfg.DirectDomains = filterNonEmpty(cfg.DirectDomains)

	return cfg, nil
}

func filterNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
