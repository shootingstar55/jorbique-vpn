//go:build !darwin && !windows

package telemetry

import (
	"context"
	"os"
	"runtime"
)

func computerName(context.Context) string {
	name, _ := os.Hostname()
	return name
}

func operatingSystem() (name, version, build string) {
	return runtime.GOOS, "", ""
}
