//go:build darwin && cgo

package netsetup

/*
#cgo LDFLAGS: -framework SystemConfiguration -framework CoreFoundation
#include <SystemConfiguration/SystemConfiguration.h>
#include <net/if.h>
#include <stdlib.h>

static char **g_names = NULL;
static int g_count = 0;

static void reset_names(void) {
	if (g_names != NULL) {
		for (int i = 0; i < g_count; i++) {
			free(g_names[i]);
		}
		free(g_names);
		g_names = NULL;
	}
	g_count = 0;
}

static int append_name(const char *name) {
	if (name == NULL || name[0] == '\0') {
		return 0;
	}
	char **next = realloc(g_names, (size_t)(g_count + 1) * sizeof(char *));
	if (next == NULL) {
		return -1;
	}
	g_names = next;
	g_names[g_count] = strdup(name);
	if (g_names[g_count] == NULL) {
		return -1;
	}
	g_count++;
	return 0;
}

// sc_list_enabled_services fills g_names with enabled network service names in system order.
static int sc_list_enabled_services(void) {
	reset_names();
	SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("jorbiquevpn"), NULL);
	if (prefs == NULL) {
		return -1;
	}
	SCNetworkSetRef set = SCNetworkSetCopyCurrent(prefs);
	if (set == NULL) {
		CFRelease(prefs);
		return -1;
	}
	CFArrayRef order = SCNetworkSetGetServiceOrder(set);
	if (order == NULL) {
		CFRelease(set);
		CFRelease(prefs);
		return -1;
	}
	CFIndex n = CFArrayGetCount(order);
	for (CFIndex i = 0; i < n; i++) {
		CFStringRef sid = CFArrayGetValueAtIndex(order, i);
		if (sid == NULL) {
			continue;
		}
		SCNetworkServiceRef svc = SCNetworkServiceCopy(prefs, sid);
		if (svc == NULL) {
			continue;
		}
		if (!SCNetworkServiceGetEnabled(svc)) {
			CFRelease(svc);
			continue;
		}
		CFStringRef name = SCNetworkServiceGetName(svc);
		if (name != NULL) {
			char buf[256];
			if (CFStringGetCString(name, buf, sizeof(buf), kCFStringEncodingUTF8)) {
				if (append_name(buf) != 0) {
					CFRelease(svc);
					break;
				}
			}
		}
		CFRelease(svc);
	}
	CFRelease(set);
	CFRelease(prefs);
	return 0;
}

static int sc_enabled_service_count(void) {
	return g_count;
}

static const char *sc_name_at(int i) {
	if (g_names == NULL || i < 0 || i >= g_count) {
		return NULL;
	}
	return g_names[i];
}

static char *sc_service_bsd_name(const char *service) {
	if (service == NULL || service[0] == '\0') {
		return NULL;
	}
	SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("jorbiquevpn"), NULL);
	if (prefs == NULL) {
		return NULL;
	}
	CFStringRef want = CFStringCreateWithCString(NULL, service, kCFStringEncodingUTF8);
	if (want == NULL) {
		CFRelease(prefs);
		return NULL;
	}
	char *out = NULL;
	CFArrayRef all = SCNetworkServiceCopyAll(prefs);
	if (all != NULL) {
		CFIndex n = CFArrayGetCount(all);
		for (CFIndex i = 0; i < n; i++) {
			SCNetworkServiceRef svc = (SCNetworkServiceRef)CFArrayGetValueAtIndex(all, i);
			if (svc == NULL) {
				continue;
			}
			CFStringRef name = SCNetworkServiceGetName(svc);
			if (name == NULL || !CFEqual(name, want)) {
				continue;
			}
			SCNetworkInterfaceRef iface = SCNetworkServiceGetInterface(svc);
			if (iface == NULL) {
				break;
			}
			CFStringRef bsd = SCNetworkInterfaceGetBSDName(iface);
			if (bsd != NULL) {
				char buf[IFNAMSIZ + 1];
				if (CFStringGetCString(bsd, buf, sizeof(buf), kCFStringEncodingUTF8)) {
					out = strdup(buf);
				}
			}
			break;
		}
		CFRelease(all);
	}
	CFRelease(want);
	CFRelease(prefs);
	return out;
}
*/
import "C"
import (
	"strings"
	"sync"
	"unsafe"
)

// scListMu serializes listEnabledNetworkServicesFromSC. The C helper stashes its results in
// process-global state (g_names/g_count), so concurrent callers — e.g. the daemon SSID/heartbeat
// goroutines and a manual start — would otherwise race on that state and corrupt the C heap.
var scListMu sync.Mutex

func listEnabledNetworkServicesFromSC() []string {
	scListMu.Lock()
	defer scListMu.Unlock()
	if C.sc_list_enabled_services() != 0 {
		return nil
	}
	defer C.reset_names()
	n := int(C.sc_enabled_service_count())
	if n == 0 {
		return nil
	}
	//nolint:gocritic // C heap pointers; copy before reset on next call.
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ptr := C.sc_name_at(C.int(i))
		if ptr == nil {
			continue
		}
		names = append(names, C.GoString(ptr))
	}
	return names
}

func bsdNameForNetworkService(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return ""
	}
	cService := C.CString(service)
	defer C.free(unsafe.Pointer(cService))
	cDev := C.sc_service_bsd_name(cService)
	if cDev == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cDev))
	return C.GoString(cDev)
}
