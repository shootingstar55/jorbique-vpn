package update

import (
	"regexp"
	"strings"

	"jorbiquevpn/internal/semver"
)

var engineVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// ShouldUpdateSingbox compares versions advertised by the configured update
// channel. Moving from SagerNet to sing-box-extended is a distribution migration:
// the fork's upstream base can be lower than the previously installed alpha.
// Once migrated, an upstream build must not silently replace the extended one.
// Within one distribution, only a newer release is installed automatically.
func ShouldUpdateSingbox(local, remote string) bool {
	local, remote = normalizeVersion(local), normalizeVersion(remote)
	if !engineVersionPattern.MatchString(remote) {
		return false
	}
	if local == "" {
		return true
	}
	if !engineVersionPattern.MatchString(local) {
		return false
	}
	localBase, localFork, localExtended := strings.Cut(local, "-extended-")
	remoteBase, remoteFork, remoteExtended := strings.Cut(remote, "-extended-")
	if remoteExtended && !engineVersionPattern.MatchString(remoteFork) {
		return false
	}
	if localExtended && !engineVersionPattern.MatchString(localFork) {
		return false
	}
	if localExtended != remoteExtended {
		return remoteExtended
	}
	if comparison := semver.Compare(remoteBase, localBase); comparison != 0 {
		return comparison > 0
	}
	return remoteExtended && semver.Compare(remoteFork, localFork) > 0
}
