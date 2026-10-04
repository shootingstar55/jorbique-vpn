//go:build !darwin || (darwin && !cgo)

package netsetup

// All darwin builds now use CGO_ENABLED=0. scnetwork_darwin.go (cgo) is dead
// code for production; this file is the active implementation on darwin.

func listEnabledNetworkServicesFromSC() []string {
	return listEnabledNetworkServicesCLI()
}

func bsdNameForNetworkService(service string) string {
	return bsdNameForNetworkServiceCLI(service)
}
