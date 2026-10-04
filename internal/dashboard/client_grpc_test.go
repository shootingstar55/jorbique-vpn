package dashboard

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeStartedService struct {
	selected *selectOutboundMessage
	tested   *urlTestMessage
	memory   uint64
}

type startedServiceServer interface{}

func authenticated(ctx context.Context) error {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 || values[0] != "Bearer test-secret" {
		return status.Error(codes.Unauthenticated, "missing token")
	}
	return nil
}

func unaryHandler[T any](decode func(any) error, request *T, ctx context.Context, run func() error) (any, error) {
	if err := decode(request); err != nil {
		return nil, err
	}
	if err := authenticated(ctx); err != nil {
		return nil, err
	}
	if err := run(); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

var testStartedService = grpc.ServiceDesc{
	ServiceName: "daemon.StartedService",
	HandlerType: (*startedServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetVersion", Handler: func(_ any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			var request emptypb.Empty
			if err := decode(&request); err != nil {
				return nil, err
			}
			if err := authenticated(ctx); err != nil {
				return nil, err
			}
			return &versionMessage{Version: "1.14.0", APIVersion: 1}, nil
		}},
		{MethodName: "SelectOutbound", Handler: func(service any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			request := new(selectOutboundMessage)
			return unaryHandler(decode, request, ctx, func() error { service.(*fakeStartedService).selected = request; return nil })
		}},
		{MethodName: "URLTest", Handler: func(service any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			request := new(urlTestMessage)
			return unaryHandler(decode, request, ctx, func() error { service.(*fakeStartedService).tested = request; return nil })
		}},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "SubscribeStatus", ServerStreams: true, Handler: func(service any, stream grpc.ServerStream) error {
			var request statusRequest
			if err := stream.RecvMsg(&request); err != nil {
				return err
			}
			if err := authenticated(stream.Context()); err != nil {
				return err
			}
			return stream.SendMsg(&statusMessage{Memory: service.(*fakeStartedService).memory, Goroutines: 7, ConnectionsIn: 2, ConnectionsOut: 3, UplinkTotal: 100, DownlinkTotal: 200})
		}},
		{StreamName: "SubscribeGroups", ServerStreams: true, Handler: func(_ any, stream grpc.ServerStream) error {
			var request emptypb.Empty
			if err := stream.RecvMsg(&request); err != nil {
				return err
			}
			return stream.SendMsg(&groupsMessage{Groups: []*groupMessage{{Tag: "proxy-selector", Type: "Selector", Selectable: true, Selected: "tuic", Items: []*groupItemMessage{{Tag: "tuic", URLTestDelay: 75}}}}})
		}},
	},
}

func TestClientUsesStartedServiceWithBearerToken(t *testing.T) {
	fake, path := startFakeStartedService(t)
	client, err := NewClient(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != "1.14.0" || snapshot.Status.Memory != 42 || snapshot.Groups[0].Selected != "tuic" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := client.SelectOutbound(context.Background(), "proxy-selector", "tuic"); err != nil {
		t.Fatal(err)
	}
	if err := client.URLTest(context.Background(), "auto"); err != nil {
		t.Fatal(err)
	}
	if fake.selected.GroupTag != "proxy-selector" || fake.selected.OutboundTag != "tuic" || fake.tested.OutboundTag != "auto" {
		t.Fatalf("actions = selected %#v, tested %#v", fake.selected, fake.tested)
	}
}

func TestLocalServerBridgesDashboardActions(t *testing.T) {
	fake, path := startFakeStartedService(t)
	bridge, err := StartLocalServer("dashboard=1", path, fstest.MapFS{"index.html": {Data: []byte("dashboard")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bridge.Close)
	baseURL := strings.Split(bridge.URL, "?")[0] + "api/"

	response, err := http.Get(baseURL + "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status = %s", response.Status)
	}
	for endpoint, body := range map[string]string{
		"select":   `["proxy-selector","tuic"]`,
		"url-test": `["auto"]`,
	} {
		response, err = http.Post(baseURL+endpoint, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("%s status = %s", endpoint, response.Status)
		}
	}
	if fake.selected.OutboundTag != "tuic" || fake.tested.OutboundTag != "auto" {
		t.Fatalf("bridge actions = selected %#v, tested %#v", fake.selected, fake.tested)
	}
}

func TestLocalServerUsesMemoryFallbackWhenSingboxReportsZero(t *testing.T) {
	fake, path := startFakeStartedService(t)
	fake.memory = 0
	bridge, err := StartLocalServer("dashboard=1", path, fstest.MapFS{"index.html": {Data: []byte("dashboard")}}, func() uint64 { return 64 << 20 })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bridge.Close)

	response, err := http.Get(strings.Split(bridge.URL, "?")[0] + "api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snapshot Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.Memory != 64<<20 {
		t.Fatalf("memory = %d, want fallback value", snapshot.Status.Memory)
	}
}

func startFakeStartedService(t *testing.T) (*fakeStartedService, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fake := &fakeStartedService{memory: 42}
	server.RegisterService(&testStartedService, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	config, _ := json.Marshal(map[string]any{"services": []any{map[string]any{"type": "api", "listen": host, "listen_port": json.Number(port), "secret": "test-secret"}}})
	path := filepath.Join(t.TempDir(), "singbox.json")
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	return fake, path
}
