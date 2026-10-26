//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

// The client no longer runs sing-box's API, but these tests switch it on in
// their own configs to watch what the core does with a selector and a urltest.
const serviceName = "/daemon.StartedService/"

type apiGroup struct {
	Tag        string         `json:"tag"`
	Type       string         `json:"type"`
	Selectable bool           `json:"selectable"`
	Selected   string         `json:"selected"`
	Items      []apiGroupItem `json:"items"`
}

type apiGroupItem struct {
	Tag          string `json:"tag"`
	Type         string `json:"type"`
	URLTestTime  int64  `json:"urlTestTime"`
	URLTestDelay int32  `json:"urlTestDelay"`
}

type apiClient struct {
	address string
	secret  string
}

func newAPIClient(configPath string) (*apiClient, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read sing-box config: %w", err)
	}
	var config struct {
		Services []struct {
			Type       string `json:"type"`
			Listen     string `json:"listen"`
			ListenPort int    `json:"listen_port"`
			Secret     string `json:"secret"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse sing-box config: %w", err)
	}
	for _, service := range config.Services {
		if service.Type != "api" {
			continue
		}
		host := service.Listen
		if host == "" {
			host = "127.0.0.1"
		}
		return &apiClient{address: net.JoinHostPort(host, fmt.Sprint(service.ListenPort)), secret: service.Secret}, nil
	}
	return nil, fmt.Errorf("sing-box API service is missing")
}

func (c *apiClient) connect() (*grpc.ClientConn, error) {
	return grpc.NewClient(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (c *apiClient) authorize(ctx context.Context) context.Context {
	if c.secret == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.secret)
}

func receive(ctx context.Context, conn *grpc.ClientConn, method string, request, response any) error {
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, serviceName+method)
	if err != nil {
		return err
	}
	if err := stream.SendMsg(request); err != nil {
		return err
	}
	if err := stream.CloseSend(); err != nil {
		return err
	}
	return stream.RecvMsg(response)
}

func (c *apiClient) Groups(ctx context.Context) ([]apiGroup, error) {
	ctx, cancel := context.WithTimeout(c.authorize(ctx), 3*time.Second)
	defer cancel()
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var response groupsMessage
	if err := receive(ctx, conn, "SubscribeGroups", &emptypb.Empty{}, &response); err != nil {
		return nil, fmt.Errorf("sing-box groups: %w", err)
	}
	return groupsFromMessage(response.Groups), nil
}

func groupsFromMessage(sources []*groupMessage) []apiGroup {
	groups := make([]apiGroup, 0, len(sources))
	for _, source := range sources {
		group := apiGroup{Tag: source.Tag, Type: source.Type, Selectable: source.Selectable, Selected: source.Selected}
		for _, item := range source.Items {
			group.Items = append(group.Items, apiGroupItem{Tag: item.Tag, Type: item.Type, URLTestTime: item.URLTestTime, URLTestDelay: item.URLTestDelay})
		}
		groups = append(groups, group)
	}
	return groups
}

func (c *apiClient) SelectOutbound(ctx context.Context, group, outbound string) error {
	if strings.TrimSpace(group) == "" || strings.TrimSpace(outbound) == "" {
		return fmt.Errorf("group and outbound are required")
	}
	return c.invoke(ctx, "SelectOutbound", &selectOutboundMessage{GroupTag: group, OutboundTag: outbound})
}

func (c *apiClient) invoke(ctx context.Context, method string, request any) error {
	ctx, cancel := context.WithTimeout(c.authorize(ctx), 3*time.Second)
	defer cancel()
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Invoke(ctx, serviceName+method, request, &emptypb.Empty{}); err != nil {
		return fmt.Errorf("sing-box API: %w", err)
	}
	return nil
}

// Minimal protobuf v1 message set for sing-box 1.14 StartedService: only the
// fields these tests read, so the tests do not import sing-box itself.

type groupsMessage struct {
	Groups []*groupMessage `protobuf:"bytes,1,rep,name=group,proto3"`
}

type groupMessage struct {
	Tag        string              `protobuf:"bytes,1,opt,name=tag,proto3"`
	Type       string              `protobuf:"bytes,2,opt,name=type,proto3"`
	Selectable bool                `protobuf:"varint,3,opt,name=selectable,proto3"`
	Selected   string              `protobuf:"bytes,4,opt,name=selected,proto3"`
	Items      []*groupItemMessage `protobuf:"bytes,6,rep,name=items,proto3"`
}

type groupItemMessage struct {
	Tag          string `protobuf:"bytes,1,opt,name=tag,proto3"`
	Type         string `protobuf:"bytes,2,opt,name=type,proto3"`
	URLTestTime  int64  `protobuf:"varint,3,opt,name=urlTestTime,proto3"`
	URLTestDelay int32  `protobuf:"varint,4,opt,name=urlTestDelay,proto3"`
}

type selectOutboundMessage struct {
	GroupTag    string `protobuf:"bytes,1,opt,name=groupTag,proto3"`
	OutboundTag string `protobuf:"bytes,2,opt,name=outboundTag,proto3"`
}

// Legacy protobuf reflection uses these methods plus field tags. gRPC adapts
// them to protobuf v2 internally.
func reset[T any](m *T) { var zero T; *m = zero }

func (m *groupsMessage) Reset()       { reset(m) }
func (*groupsMessage) String() string { return "" }
func (*groupsMessage) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
func (m *groupMessage) Reset()       { reset(m) }
func (*groupMessage) String() string { return "" }
func (*groupMessage) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
func (m *groupItemMessage) Reset()       { reset(m) }
func (*groupItemMessage) String() string { return "" }
func (*groupItemMessage) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
func (m *selectOutboundMessage) Reset()       { reset(m) }
func (*selectOutboundMessage) String() string { return "" }
func (*selectOutboundMessage) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
