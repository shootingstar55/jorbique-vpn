package core

type CLIErrorKind int

const (
	ErrUsage CLIErrorKind = iota
	ErrMissing
	ErrBadConfig
	ErrToolNotFound
	ErrStartFailed
	ErrPermission
	ErrUnsupportedBuild
)

type CLIError struct {
	Kind               CLIErrorKind
	Msg                string
	SuggestionOverride string
}

func (e *CLIError) Error() string {
	switch e.Kind {
	case ErrUsage:
		return e.Msg
	case ErrMissing:
		return "Missing: " + e.Msg
	case ErrBadConfig:
		return "Bad config: " + e.Msg
	case ErrToolNotFound:
		return "Tool not found: " + e.Msg
	case ErrStartFailed:
		return "Start failed: " + e.Msg
	case ErrPermission:
		return "Permission error: " + e.Msg
	case ErrUnsupportedBuild:
		return e.Msg
	}
	return e.Msg
}

func (e *CLIError) ExitCode() int {
	switch e.Kind {
	case ErrUsage:
		return ExitUsage
	case ErrMissing:
		return ExitNoInput
	case ErrBadConfig:
		return ExitConfig
	case ErrToolNotFound:
		return ExitDataErr
	case ErrPermission:
		return ExitPermission
	case ErrStartFailed:
		return ExitGeneral
	case ErrUnsupportedBuild:
		return ExitGeneral
	}
	return ExitGeneral
}

func (e *CLIError) Suggestion() string {
	if e.SuggestionOverride != "" {
		return e.SuggestionOverride
	}
	switch e.Kind {
	case ErrUsage:
		return "Run 'jorbique-vpn help' for usage."
	case ErrMissing:
		return "Ensure the file exists at " + e.Msg + "."
	case ErrBadConfig:
		return "Check cfg/vpn_cfg.json for syntax and required fields."
	case ErrToolNotFound:
		return "Install the sing-box core from Jorbique VPN settings"
	case ErrPermission:
		return "Run with sudo for install-autostart/uninstall-autostart."
	case ErrStartFailed:
		return "Check logs in logs/ and verify config."
	case ErrUnsupportedBuild:
		return ""
	}
	return ""
}

func Usage(msg string) error        { return &CLIError{Kind: ErrUsage, Msg: msg} }
func Missing(msg string) error      { return &CLIError{Kind: ErrMissing, Msg: msg} }
func BadConfig(msg string) error    { return &CLIError{Kind: ErrBadConfig, Msg: msg} }
func ToolNotFound(msg string) error { return &CLIError{Kind: ErrToolNotFound, Msg: msg} }
func StartFailed(msg string) error  { return &CLIError{Kind: ErrStartFailed, Msg: msg} }
func Permission(msg string) error   { return &CLIError{Kind: ErrPermission, Msg: msg} }

// UnsupportedBuild is for binaries missing native features (e.g. daemon mode without CGO).
func UnsupportedBuild(msg, suggestion string) error {
	return &CLIError{Kind: ErrUnsupportedBuild, Msg: msg, SuggestionOverride: suggestion}
}

func ExitCodeFor(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if cli, ok := err.(*CLIError); ok {
		return cli.ExitCode()
	}
	return ExitGeneral
}
