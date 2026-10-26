package constants

import "testing"

// Both clients fetch the binary manifest and the profile from the server's
// extended channel, one directory under the configured host.
func TestSingboxEndpoints(t *testing.T) {
	channel := "https://" + ServerHost + ExtendedUpdatePath
	if SingboxManifestURL != channel+"version.json" {
		t.Errorf("manifest URL = %q", SingboxManifestURL)
	}
	if SingboxClientURL != channel+"singbox_client.json" {
		t.Errorf("client URL = %q", SingboxClientURL)
	}
}
