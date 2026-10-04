// Package integration holds end-to-end tests that exercise multiple internal
// packages together (config build pipeline, optional real sing-box validation).
//
// They are guarded by the "integration" build tag so the default `go test ./...`
// (and CI unit coverage) does not run them. Run them explicitly:
//
//	go test -tags integration ./test/integration/...
//
// Set SINGBOX_BIN to a sing-box executable to additionally validate the generated
// runtime config with `sing-box check`.
package integration
