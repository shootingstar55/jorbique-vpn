package localapi

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeStartedService struct {
	selected *selectOutboundMessage
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
		{MethodName: "SelectOutbound", Handler: func(service any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			request := new(selectOutboundMessage)
			return unaryHandler(decode, request, ctx, func() error { service.(*fakeStartedService).selected = request; return nil })
		}},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "SubscribeStatus", ServerStreams: true, Handler: func(_ any, stream grpc.ServerStream) error {
			var request statusRequest
			if err := stream.RecvMsg(&request); err != nil {
				return err
			}
			if err := authenticated(stream.Context()); err != nil {
				return err
			}
			return stream.SendMsg(&statusMessage{UplinkTotal: 100, DownlinkTotal: 200})
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
	traffic, err := client.Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if traffic != (Traffic{UplinkTotal: 100, DownlinkTotal: 200}) {
		t.Fatalf("traffic = %#v", traffic)
	}
	group, err := client.Selector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if group.Tag != "proxy-selector" || group.Selected != "tuic" || group.Items[0].URLTestDelay != 75 {
		t.Fatalf("selector = %#v", group)
	}
	if err := client.SelectOutbound(context.Background(), "proxy-selector", "tuic"); err != nil {
		t.Fatal(err)
	}
	if fake.selected.GroupTag != "proxy-selector" || fake.selected.OutboundTag != "tuic" {
		t.Fatalf("selected %#v", fake.selected)
	}
}

func startFakeStartedService(t *testing.T) (*fakeStartedService, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fake := &fakeStartedService{}
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
