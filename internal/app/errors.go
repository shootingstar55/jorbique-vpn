package app

import "errors"

var (
	ErrInvalidStackMode  = errors.New("invalid stack mode")
	ErrVlessRequired     = errors.New("VLESS URLs required")
	ErrSingboxNotFound   = errors.New("sing-box not found")
	ErrStartupFailed     = errors.New("startup failed")
	ErrInvalidVless      = errors.New("invalid VLESS URL")
	ErrConfigUpdate      = errors.New("config update failed")
	ErrUnknownUUID       = errors.New("unknown user UUID")
	ErrServerUnreachable = errors.New("server unreachable")
)
