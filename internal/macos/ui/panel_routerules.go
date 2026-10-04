//go:build darwin

package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/macos/applekit"
)

// routeRulesState is what the route-rules window opens with: the domain fields
// as typed, and the bypass paths already grouped by the application they belong
// to — the grouping is a filesystem walk, which belongs on this side.
type routeRulesState struct {
	Direct    string         `json:"direct"`
	Processes string         `json:"processes"`
	Apps      []routeRuleApp `json:"apps"`
}

type routeRuleApp struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

type routeRulesUpdate struct {
	Direct    string   `json:"direct"`
	Processes string   `json:"processes"`
	Paths     []string `json:"paths"`
}

// showRouteRulesPanel presents the route rules as a page in the shared bundle.
func showRouteRulesPanel(init routeRulesInput, onSubmit func(routeRulesInput) error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	state := routeRulesState{
		Direct:    init.Direct,
		Processes: init.Processes,
		Apps:      routeRuleAppsOf(init.Paths),
	}
	return showWebPanel("Route rules", 640, 560, 520, 440,
		func(closeWindow func()) (*dashboard.LocalServer, error) {
			return dashboard.StartFormServerWithActions(panelQuery("route-rules"), "route-rules", assets, state,
				func(body json.RawMessage) error {
					var update routeRulesUpdate
					if err := json.Unmarshal(body, &update); err != nil {
						return fmt.Errorf("invalid request")
					}
					if onSubmit == nil {
						return nil
					}
					return onSubmit(routeRulesInput{
						Direct:    update.Direct,
						Processes: update.Processes,
						Paths:     update.Paths,
					})
				},
				map[string]func(json.RawMessage) (any, error){
					// The page cannot open a file chooser; this runs the native
					// one on the main thread and hands back the list it produced.
					"pick-apps": func(body json.RawMessage) (any, error) {
						var keep []string
						_ = json.Unmarshal(body, &keep)
						return map[string]any{"apps": pickApplications(keep)}, nil
					},
				},
				closeWindow)
		}, onClosed)
}

// pickApplications shows the chooser and returns the groups to display: the ones
// already listed, plus whatever was chosen.
func pickApplications(keep []string) []routeRuleApp {
	chosen := make(chan []string, 1)
	applekit.RunOnMain(func() { chosen <- applekit.ChooseApplicationFiles() })
	paths := append([]string(nil), keep...)
	for _, url := range <-chosen {
		if !strings.EqualFold(filepath.Ext(url), ".app") {
			continue
		}
		paths = append(paths, expandedPathsForChosenApp(url)...)
	}
	return routeRuleAppsOf(paths)
}

// routeRuleAppsOf groups flat executable paths the way the list shows them.
func routeRuleAppsOf(paths []string) []routeRuleApp {
	groups := groupsFromFlatPaths(paths)
	apps := make([]routeRuleApp, 0, len(groups))
	for _, group := range groups {
		apps = append(apps, routeRuleApp{ID: group.groupID, Name: group.displayName, Paths: group.paths})
	}
	return apps
}
