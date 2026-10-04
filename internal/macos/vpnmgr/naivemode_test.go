package vpnmgr

import (
	"os"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// The mode is set-singbox-config's fourth line: "naive", "standard", or blank
// for "keep what is stored".
func TestSetSingboxConfigStoresTheMode(t *testing.T) {
	m := newFirstRunManager(t)
	if err := m.SetSingboxConfig([]string{"", "", "", "naive"}); err != nil {
		t.Fatalf("saving the NaiveProxy mode: %v", err)
	}
	if !m.Cfg.Singbox.NaiveMode {
		t.Fatal("the NaiveProxy mode was not stored")
	}
	// A save that says nothing about the mode keeps it.
	if err := m.SetSingboxConfig([]string{"", "", ""}); err != nil {
		t.Fatal(err)
	}
	if !m.Cfg.Singbox.NaiveMode {
		t.Error("a save without a mode line switched the NaiveProxy mode off")
	}
	if err := m.SetSingboxConfig([]string{"", "", "", "standard"}); err != nil {
		t.Fatal(err)
	}
	if m.Cfg.Singbox.NaiveMode {
		t.Error("the protocols mode was not stored")
	}
	if err := m.SetSingboxConfig([]string{"", "", "", "naiv"}); err == nil {
		t.Error("an unknown mode was accepted")
	}
	saved, err := os.ReadFile(m.Paths.CfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(saved), `"naive_mode"`) != 1 {
		t.Errorf("the mode key should appear once:\n%s", saved)
	}
}

func TestHasBuildTag(t *testing.T) {
	const out = "sing-box version 1.15.0-alpha.10\n\nEnvironment: go1.26.8 darwin/arm64\n" +
		"Tags: with_gvisor,with_quic,with_naive_outbound,badlinkname\nRevision: abc\n"
	if !hasBuildTag(out, naiveBuildTag) {
		t.Error("with_naive_outbound was not found on the Tags line")
	}
	if hasBuildTag(strings.Replace(out, "with_naive_outbound,", "", 1), naiveBuildTag) {
		t.Error("a build without naive was reported as having it")
	}
	// Only the Tags line counts, and only whole tags.
	if hasBuildTag("Revision: with_naive_outbound\nTags: with_naive_outbound_x\n", naiveBuildTag) {
		t.Error("a tag was matched outside the Tags line or by prefix")
	}
}

// With no binary that can run naive and nowhere to download one from, the
// NaiveProxy mode refuses to start with a reason rather than a sing-box that
// exits on its config.
func TestEnsureNaiveSingboxRefusesABuildWithoutNaive(t *testing.T) {
	m := newFirstRunManager(t)
	m.Paths.BinDir = ""
	err := m.ensureNaiveSingbox()
	if err == nil || !strings.Contains(err.Error(), "naive") {
		t.Fatalf("ensureNaiveSingbox = %v, want a refusal naming naive", err)
	}
	var cli *core.CLIError
	if !asCLIError(err, &cli) || cli.Kind != core.ErrStartFailed {
		t.Errorf("want a start failure, got %#v", err)
	}
}

func asCLIError(err error, target **core.CLIError) bool {
	c, ok := err.(*core.CLIError)
	if ok {
		*target = c
	}
	return ok
}
