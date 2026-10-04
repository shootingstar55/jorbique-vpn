package ui

import (
	"errors"
	"fmt"
	"testing"

	"jorbiquevpn/internal/app"
)

func TestErrorWrapPreservesSentinel(t *testing.T) {
	base := app.ErrConfigUpdate
	wrapped := fmt.Errorf("user-visible: %w", base)
	if !errors.Is(wrapped, app.ErrConfigUpdate) {
		t.Fatal("errors.Is should see sentinel through fmt.Errorf %%w")
	}
}
