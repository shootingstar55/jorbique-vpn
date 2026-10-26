package autoconnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jorbiquevpn/internal/dashboard"
)

// Activate applies the measured winner after startup as well as in the JSON.
// sing-box can restore a selector from its persistent DNS/cache database before
// consulting "default". Keep that useful DNS cache, but override old choices.
func (r Result) Activate(ctx context.Context, configPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client, err := dashboard.NewClient(configPath)
	if err != nil {
		return err
	}
	for {
		groups, err := client.Groups(ctx)
		if err == nil {
			for _, group := range groups {
				if !strings.EqualFold(group.Type, "selector") {
					continue
				}
				if err := client.SelectOutbound(ctx, group.Tag, r.Winner()); err != nil {
					return err
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("could not activate the checked connection")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
