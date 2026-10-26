package autoconnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jorbiquevpn/internal/dashboard"
)

// Activate applies the configured default after startup as well as in the JSON.
// sing-box can restore a selector from its persistent DNS/cache database before
// consulting "default". Keep that useful DNS cache, but override old choices:
// a selector goes to its urltest (failover) when it has one, else the winner.
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
				target := r.Winner()
				for _, item := range group.Items {
					if strings.EqualFold(item.Type, "urltest") {
						target = item.Tag
						break
					}
				}
				if err := client.SelectOutbound(ctx, group.Tag, target); err != nil {
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
