package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/google/uuid"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"jorbiquevpn/internal/constants"
)

func TestDeviceMetadataReachesOTLPAndFollowsRenamesAndCoreUpdates(t *testing.T) {
	var requests []*collectormetrics.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		request := new(collectormetrics.ExportMetricsServiceRequest)
		if err := proto.Unmarshal(body, request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer func(old string) { endpoint = old }(endpoint)
	endpoint = srv.URL + "/otlp/v1/metrics"
	version := ""
	id := uuid.NewString()
	exp, err := newExporter(&authTransport{
		uuid: func() string { return id }, logf: func(string) {},
	}, func() string { return version })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hostName := computerName(ctx)
	if (runtime.GOOS == "darwin" || runtime.GOOS == "windows") && hostName == "" {
		t.Fatal("system computer name unavailable")
	}
	exp.(*deviceMetadataExporter).name = func(context.Context) string { return hostName }
	defer func() { _ = exp.Shutdown(ctx) }()
	reader := sdkmetric.NewManualReader()
	defer func() { _ = reader.Shutdown(ctx) }()
	dir := t.TempDir()
	if err := observeTunnels(dir, reader, func() map[string]bool { return map[string]bool{"sing-box": true} }); err != nil {
		t.Fatal(err)
	}
	name, release, build := operatingSystem()
	if runtime.GOOS == "darwin" && (name != "macOS" || release == "" || build == "") {
		t.Fatalf("macOS product version/build unavailable: %q %q %q", name, release, build)
	}
	for _, current := range []struct{ version, name string }{
		{"", hostName},
		{"1.14.0-alpha.2+custom.1", "Егор’s MacBook Air"},
		{"1.14.1", "OFFICE-PC"},
		{"1.14.1", ""}, // lookup failure must not prevent the metrics push
	} {
		version, hostName = current.version, current.name
		var data metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &data); err != nil {
			t.Fatal(err)
		}
		if err := exp.Export(ctx, &data); err != nil {
			t.Fatal(err)
		}
		request := requests[len(requests)-1]
		if len(request.ResourceMetrics) != 1 {
			t.Fatalf("resources = %d", len(request.ResourceMetrics))
		}
		rm := request.ResourceMetrics[0]
		attrs := map[string]string{}
		for _, a := range rm.Resource.Attributes {
			attrs[a.Key] = a.Value.GetStringValue()
		}
		for key, want := range map[string]string{
			"service.name": "jorbique-vpn", "service.version": constants.AppVersion,
			"service.instance.id": instanceID(dir), "singbox.version": knownVersion(current.version),
			"host.name": current.name,
			"os.type":   runtime.GOOS, "os.name": name,
			"os.version": knownVersion(release), "os.build.id": knownVersion(build),
		} {
			if attrs[key] != want {
				t.Errorf("%s = %q, want %q", key, attrs[key], want)
			}
		}
		point := rm.ScopeMetrics[0].Metrics[0].GetGauge().DataPoints[0]
		if point.GetAsInt() != 1 || len(point.Attributes) != 1 || point.Attributes[0].Key != "vpn" {
			t.Fatalf("device metadata leaked into tunnel identity: %v", point)
		}
	}
}

func TestWindowsNamesUseNativeBuildAndProductType(t *testing.T) {
	for _, tc := range []struct {
		major, minor, build uint32
		workstation         bool
		want                string
	}{
		{10, 0, 19045, true, "Windows 10"},
		{10, 0, 21999, true, "Windows 10"},
		{10, 0, 22000, true, "Windows 11"},
		{10, 0, 26100, true, "Windows 11"},
		{10, 0, 26100, false, "Windows Server"},
		{6, 3, 9600, true, "Windows 8.1"},
	} {
		if got := windowsName(tc.major, tc.minor, tc.build, tc.workstation); got != tc.want {
			t.Errorf("build %d, workstation %v: %q, want %q", tc.build, tc.workstation, got, tc.want)
		}
	}
}

func TestSetDeviceOverridesThePlatform(t *testing.T) {
	t.Cleanup(func() { SetDevice("", "", "", "") })
	SetDevice(" Pixel 9 ", "Android", "15", "AP3A.241005.015")
	if got := deviceName(context.Background()); got != "Pixel 9" {
		t.Fatalf("deviceName = %q", got)
	}
	if name, version, build := deviceOS(); name != "Android" || version != "15" || build != "AP3A.241005.015" {
		t.Fatalf("deviceOS = %q %q %q", name, version, build)
	}
	SetDevice("", "", "", "")
	if got, want := deviceName(context.Background()), computerName(context.Background()); got != want {
		t.Fatalf("deviceName without an override = %q, want the platform's %q", got, want)
	}
}
