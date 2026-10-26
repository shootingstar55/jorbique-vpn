//go:build tools

package core

// gomobile bind generates its glue against this package, so the module must
// require it at the version cmd/gomobile is installed from (build-core.sh).
import _ "github.com/sagernet/gomobile/bind"
