package proto

import (
	context "context"
	"strings"

	grpc "google.golang.org/grpc"
)

type TnetTunelClientX interface {
	TunnelX(ctx context.Context, method string, opts ...grpc.CallOption) (TnetTunel_TunnelClient, error)
}
type tnetTunelClientX struct {
	cc grpc.ClientConnInterface
}

func NewTnetTunelClientX(cc grpc.ClientConnInterface) TnetTunelClientX {
	return &tnetTunelClientX{
		cc: cc,
	}
}

func (c *tnetTunelClientX) TunnelX(ctx context.Context, method string, opts ...grpc.CallOption) (TnetTunel_TunnelClient, error) {
	sd := ServerDesc(method)
	method = "/" + sd.ServiceName + "/" + sd.Streams[0].StreamName
	stream, err := c.cc.NewStream(ctx, &sd.Streams[0], method, opts...)
	if err != nil {
		return nil, err
	}
	x := &tnetTunelTunnelClient{stream}
	return x, nil
}

func RegisterTnetTunelServerX(s grpc.ServiceRegistrar, srv TnetTunelServer, method string) {
	sd := ServerDesc(method)
	s.RegisterService(&sd, srv)
}

func ServerDesc(method string) grpc.ServiceDesc {
	serviceName, streamName := parsingMethod(method)

	return grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: TnetTunel_ServiceDesc.HandlerType,
		Methods:     TnetTunel_ServiceDesc.Methods,
		Streams: []grpc.StreamDesc{
			{
				StreamName:    streamName,
				Handler:       TnetTunel_ServiceDesc.Streams[0].Handler,
				ServerStreams: TnetTunel_ServiceDesc.Streams[0].ServerStreams,
				ClientStreams: TnetTunel_ServiceDesc.Streams[0].ClientStreams,
			},
		},
		Metadata: TnetTunel_ServiceDesc.Metadata,
	}

}

func parsingMethod(method string) (string, string) {
	serviceName := TnetTunel_ServiceDesc.ServiceName
	streamName := TnetTunel_ServiceDesc.Streams[0].StreamName
	v := strings.SplitN(strings.Trim(method, "/"), "/", 2)
	if len(v) == 1 && v[0] != "" {
		serviceName = v[0]
	}
	if len(v) == 2 {
		serviceName = v[0]
		streamName = strings.Replace(v[1], "/", "-", -1)
	}

	return serviceName, streamName
}
