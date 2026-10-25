package constants

import (
	"net/url"
	"testing"
)

// Both clients fetch the binary manifest and the profile from the server's
// extended channel, one directory under the configured host.
func TestSingboxEndpoints(t *testing.T) {
	channel := "https://" + ServerHost + "/chI6dwCVntNE-extended/"
	if SingboxManifestURL != channel+"version.json" {
		t.Errorf("manifest URL = %q", SingboxManifestURL)
	}
	if SingboxClientURL != channel+"singbox_client.json" {
		t.Errorf("client URL = %q", SingboxClientURL)
	}
}

// Older client binaries have the original path compiled in. Its profile and
// manifest must stay independent of the new engine distribution's channel.
func TestExtendedChannelDoesNotReuseLegacyURLs(t *testing.T) {
	const legacyPath = "/chI6dwCVntNE/"
	if ExtendedUpdatePath == legacyPath {
		t.Fatal("extended builds must use a separate channel from installed legacy clients")
	}
	for _, endpoint := range []string{SingboxManifestURL, SingboxClientURL} {
		u, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if u.Scheme != "https" || u.Host != ServerHost {
			t.Fatalf("endpoint does not use the configured HTTPS host: %q", endpoint)
		}
		for _, file := range []string{"version.json", "singbox_client.json"} {
			if u.Path == legacyPath+file {
				t.Fatalf("new client points at legacy channel: %q", endpoint)
			}
		}
	}
}
