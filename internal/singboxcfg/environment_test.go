package singboxcfg

import (
	"reflect"
	"testing"
)

func TestEngineEnvironmentRemovesInheritedProxies(t *testing.T) {
	input := []string{"PATH=/bin", "https_proxy=http://proxy.invalid", "HTTP_PROXY=http://proxy.invalid", "ALL_PROXY=socks5://proxy.invalid", "NO_PROXY=localhost", "USER=fixture"}
	want := []string{"PATH=/bin", "NO_PROXY=localhost", "USER=fixture"}
	if got := EngineEnvironment(input); !reflect.DeepEqual(got, want) || len(input) != 6 {
		t.Fatalf("environment = %v", got)
	}
}
