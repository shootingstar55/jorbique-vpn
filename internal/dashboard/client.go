package dashboard

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

const (
	Port        = 39217
	serviceName = "/daemon.StartedService/"
)

type Snapshot struct {
	Version string  `json:"version"`
	Status  Status  `json:"status"`
	Groups  []Group `json:"groups"`
}

type Status struct {
	Memory           uint64 `json:"memory"`
	Goroutines       int32  `json:"goroutines"`
	ConnectionsIn    int32  `json:"connectionsIn"`
	ConnectionsOut   int32  `json:"connectionsOut"`
	TrafficAvailable bool   `json:"trafficAvailable"`
	Uplink           int64  `json:"uplink"`
	Downlink         int64  `json:"downlink"`
	UplinkTotal      int64  `json:"uplinkTotal"`
	DownlinkTotal    int64  `json:"downlinkTotal"`
}

type Group struct {
	Tag        string      `json:"tag"`
	Type       string      `json:"type"`
	Selectable bool        `json:"selectable"`
	Selected   string      `json:"selected"`
	Items      []GroupItem `json:"items"`
}

type GroupItem struct {
	Tag          string `json:"tag"`
	Type         string `json:"type"`
	URLTestTime  int64  `json:"urlTestTime"`
	URLTestDelay int32  `json:"urlTestDelay"`
}

type Client struct {
	address string
	secret  string
}

func NewClient(configPath string) (*Client, error) {
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
		port := service.ListenPort
		if port == 0 {
			port = Port
		}
		return &Client{address: net.JoinHostPort(host, fmt.Sprint(port)), secret: service.Secret}, nil
	}
	return nil, fmt.Errorf("sing-box API service is missing")
}

func (c *Client) connect() (*grpc.ClientConn, error) {
	return grpc.NewClient(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (c *Client) authorize(ctx context.Context) context.Context {
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

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(c.authorize(ctx), 3*time.Second)
	defer cancel()
	conn, err := c.connect()
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()

	var version versionMessage
	if err := conn.Invoke(ctx, serviceName+"GetVersion", &emptypb.Empty{}, &version); err != nil {
		return Snapshot{}, fmt.Errorf("sing-box API: %w", err)
	}
	var status statusMessage
	if err := receive(ctx, conn, "SubscribeStatus", &statusRequest{Interval: int64(time.Second)}, &status); err != nil {
		return Snapshot{}, fmt.Errorf("sing-box status: %w", err)
	}
	var groups groupsMessage
	if err := receive(ctx, conn, "SubscribeGroups", &emptypb.Empty{}, &groups); err != nil {
		return Snapshot{}, fmt.Errorf("sing-box groups: %w", err)
	}
	return Snapshot{
		Version: version.Version,
		Status:  Status(status),
		Groups:  groupsFromMessage(groups.Groups),
	}, nil
}

func (c *Client) Groups(ctx context.Context) ([]Group, error) {
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

func groupsFromMessage(sources []*groupMessage) []Group {
	groups := make([]Group, 0, len(sources))
	for _, source := range sources {
		group := Group{Tag: source.Tag, Type: source.Type, Selectable: source.Selectable, Selected: source.Selected}
		for _, item := range source.Items {
			group.Items = append(group.Items, GroupItem{Tag: item.Tag, Type: item.Type, URLTestTime: item.URLTestTime, URLTestDelay: item.URLTestDelay})
		}
		groups = append(groups, group)
	}
	return groups
}

func (c *Client) SelectOutbound(ctx context.Context, group, outbound string) error {
	if strings.TrimSpace(group) == "" || strings.TrimSpace(outbound) == "" {
		return fmt.Errorf("group and outbound are required")
	}
	return c.invoke(ctx, "SelectOutbound", &selectOutboundMessage{GroupTag: group, OutboundTag: outbound})
}

func (c *Client) URLTest(ctx context.Context, outbound string) error {
	if strings.TrimSpace(outbound) == "" {
		return fmt.Errorf("outbound is required")
	}
	return c.invoke(ctx, "URLTest", &urlTestMessage{OutboundTag: outbound})
}

func (c *Client) invoke(ctx context.Context, method string, request any) error {
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

func (c *Client) Selector(ctx context.Context) (Group, error) {
	groups, err := c.Groups(ctx)
	if err != nil {
		return Group{}, err
	}
	for _, group := range groups {
		if group.Selectable || strings.EqualFold(group.Type, "selector") {
			return group, nil
		}
	}
	return Group{}, fmt.Errorf("running config has no selector")
}
