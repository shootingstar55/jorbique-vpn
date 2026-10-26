package localapi

// Minimal protobuf v1 message set for sing-box 1.14 StartedService. Keeping
// only fields the client uses avoids importing sing-box itself into client.

type statusRequest struct {
	Interval int64 `protobuf:"varint,1,opt,name=interval,proto3"`
}

type statusMessage struct {
	UplinkTotal   int64 `protobuf:"varint,8,opt,name=uplinkTotal,proto3"`
	DownlinkTotal int64 `protobuf:"varint,9,opt,name=downlinkTotal,proto3"`
}

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

func (m *statusRequest) Reset()       { reset(m) }
func (*statusRequest) String() string { return "" }
func (*statusRequest) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
func (m *statusMessage) Reset()       { reset(m) }
func (*statusMessage) String() string { return "" }
func (*statusMessage) ProtoMessage() {
	// Marker method required by the protobuf v1 message interface.
}
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
