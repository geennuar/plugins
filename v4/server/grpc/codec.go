package grpc

import (
	"encoding/json"
	"fmt"
	"strings"

	"go-micro.dev/v4/codec"
	"go-micro.dev/v4/codec/bytes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/runtime/protoiface"
	"google.golang.org/protobuf/runtime/protoimpl"
)

type jsonCodec struct{}
type bytesCodec struct{}
type protoCodec struct{}
type wrapCodec struct{ encoding.Codec }

var marshalOptions = &protojson.MarshalOptions{
	AllowPartial:    true,
	UseProtoNames:   true,
	UseEnumNumbers:  false,
	EmitUnpopulated: true,
}

var unmarshalOptions = &protojson.UnmarshalOptions{
	AllowPartial:   true,
	DiscardUnknown: true,
}

var (
	defaultGRPCCodecs = map[string]encoding.Codec{
		"application/json":         jsonCodec{},
		"application/proto":        protoCodec{},
		"application/protobuf":     protoCodec{},
		"application/octet-stream": protoCodec{},
		"application/grpc":         protoCodec{},
		"application/grpc+json":    jsonCodec{},
		"application/grpc+proto":   protoCodec{},
		"application/grpc+bytes":   bytesCodec{},
	}
)

func (w wrapCodec) String() string {
	return w.Codec.Name()
}

func (w wrapCodec) Marshal(v interface{}) ([]byte, error) {
	b, ok := v.(*bytes.Frame)
	if ok {
		return b.Data, nil
	}
	return w.Codec.Marshal(v)
}

func (w wrapCodec) Unmarshal(data []byte, v interface{}) error {
	b, ok := v.(*bytes.Frame)
	if ok {
		b.Data = data
		return nil
	}
	if v == nil {
		return nil
	}
	return w.Codec.Unmarshal(data, v)
}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	switch m := v.(type) {
	case *bytes.Frame:
		return m.Data, nil
	case proto.Message:
		return proto.Marshal(m)
	case protoiface.MessageV1:
		// #2333 compatible with etcd legacy proto.Message
		m2 := protoimpl.X.ProtoMessageV2Of(m)
		return proto.Marshal(m2)
	}
	return nil, fmt.Errorf("failed to marshal: %v is not type of *bytes.Frame or proto.Message", v)
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	switch m := v.(type) {
	case proto.Message:
		return proto.Unmarshal(data, m)
	case protoiface.MessageV1:
		// #2333 compatible with etcd legacy proto.Message
		m2 := protoimpl.X.ProtoMessageV2Of(m)
		return proto.Unmarshal(data, m2)
	}
	return fmt.Errorf("failed to unmarshal: %v is not type of proto.Message", v)
}

func (protoCodec) Name() string {
	return "proto"
}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	if pb, ok := v.(proto.Message); ok {
		s, err := marshalOptions.Marshal(pb)
		return []byte(s), err
	}

	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	if pb, ok := v.(proto.Message); ok {
		return unmarshalOptions.Unmarshal(data, pb)
	}
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return "json"
}

func (bytesCodec) Marshal(v interface{}) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, codec.ErrInvalidMessage
	}
	return *b, nil
}

func (bytesCodec) Unmarshal(data []byte, v interface{}) error {
	b, ok := v.(*[]byte)
	if !ok {
		return codec.ErrInvalidMessage
	}
	*b = data
	return nil
}

func (bytesCodec) Name() string {
	return "bytes"
}

type grpcCodec struct {
	// headers
	id       string
	target   string
	method   string
	endpoint string

	s grpc.ServerStream
	c encoding.Codec
}

func (g *grpcCodec) ReadHeader(m *codec.Message, mt codec.MessageType) error {
	md, _ := metadata.FromIncomingContext(g.s.Context())
	if m == nil {
		m = new(codec.Message)
	}
	if m.Header == nil {
		m.Header = make(map[string]string, len(md))
	}
	for k, v := range md {
		m.Header[k] = strings.Join(v, ",")
	}
	m.Id = g.id
	m.Target = g.target
	m.Method = g.method
	m.Endpoint = g.endpoint
	return nil
}

func (g *grpcCodec) ReadBody(v interface{}) error {
	// caller has requested a frame
	if f, ok := v.(*bytes.Frame); ok {
		return g.s.RecvMsg(f)
	}
	return g.s.RecvMsg(v)
}

func (g *grpcCodec) Write(m *codec.Message, v interface{}) error {
	// if we don't have a body
	if v != nil {
		b, err := g.c.Marshal(v)
		if err != nil {
			return err
		}
		m.Body = b
	}
	// write the body using the framing codec
	return g.s.SendMsg(&bytes.Frame{Data: m.Body})
}

func (g *grpcCodec) Close() error {
	return nil
}

func (g *grpcCodec) String() string {
	return "grpc"
}
