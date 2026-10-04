package cli

import (
	"reflect"
	"testing"
)

// stop reaches the manager with the explicit names after trimming.
// Nothing left must stop nothing — not index into an empty list, and not fall
// through to "all".
func TestStopDispatchesWhatWasNamed(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want []string
	}{
		{[]string{"stop"}, nil},
		{[]string{"stop", " ", ""}, nil},
		{[]string{"stop", "all"}, []string{"all"}},
		{[]string{"stop", " all ", "intra"}, []string{"all"}},
		{[]string{"stop", "intra", "all"}, []string{"intra", "all"}},
		{[]string{"stop", " intra ", "", "projects"}, []string{"intra", "projects"}},
	} {
		var stopped []string
		stopNamed(nonBlank(tt.args[1:]), func(name string) { stopped = append(stopped, name) })
		if !reflect.DeepEqual(stopped, tt.want) {
			t.Errorf("%q stopped %q, want %q", tt.args, stopped, tt.want)
		}
	}
}

func TestBareLifecycleCommandsTargetSingbox(t *testing.T) {
	for _, cmd := range []string{"start", "stop", "restart"} {
		for _, args := range [][]string{{cmd}, {cmd, " ", ""}} {
			if got := tunnelArgs(cmd, args); !reflect.DeepEqual(got, []string{cmd, "all"}) {
				t.Errorf("%v resolved to %v", args, got)
			}
		}
	}
	for _, args := range [][]string{{"status"}, {"start", "Jorbique VPN"}, {"stop", "all"}} {
		if got := tunnelArgs(args[0], args); !reflect.DeepEqual(got, args) {
			t.Errorf("explicit command changed: %v -> %v", args, got)
		}
	}
}
