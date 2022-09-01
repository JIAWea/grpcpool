package grpcpool

import (
	"context"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"reflect"
	"testing"
)

func TestExtractName(t *testing.T) {
	s := &ServiceClientPool{}
	name := s.ExtractServiceName("/account/Register")

	if !reflect.DeepEqual(name, "/account") {
		t.Errorf("got: %v, expect: %v", name, "/account")
	}
}

func TestHostServiceNames(t *testing.T) {
	m := NewTargetServiceNames()
	m.Set("127.0.0.1:8080", "/account", "/order")

	if !reflect.DeepEqual(m.List()["127.0.0.1:8080"][0], "/account") {
		t.Errorf("got: %v, expect: %v", m.List()["127.0.0.1:8080"][0], "/account")
	}
	if !reflect.DeepEqual(m.List()["127.0.0.1:8080"][1], "/order") {
		t.Errorf("got: %v, expect: %v", m.List()["127.0.0.1:8080"][1], "/order")
	}
}

func TestNewServiceClientPool(t *testing.T) {
	type args struct {
		option *ClientOption
	}
	tests := []struct {
		name string
		args args
		uri  string
	}{
		{
			name: "test1",
			args: args{
				option: NewDefaultClientOption(),
			},
			uri: "localhost:50051/helloworld.Greeter/SayHello",
		},
		{
			name: "test2",
			args: args{
				option: NewDefaultClientOption(),
			},
			uri: "localhost:50052/helloworld.Greeter/SayHello",
		},
		{
			name: "test3",
			args: args{
				option: NewDefaultClientOption(),
			},
			uri: "localhost:50053/helloworld.Greeter/SayHello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewServiceClientPool(tt.args.option)

			m := NewTargetServiceNames()
			m.Set("127.0.0.1:50051", []string{"/helloworld.Greeter"}...)
			m.Set("127.0.0.1:50052", []string{"/helloworld.Greeter"}...)
			m.Set("127.0.0.1:50053", []string{"/helloworld.Greeter"}...)
			got.Init(*m)

			in := &pb.HelloRequest{Name: "ray"}
			out := &pb.HelloReply{}
			err := got.Invoke(context.Background(), tt.uri, map[string]string{}, in, out)
			if err != nil {
				t.Errorf("err: %v", err)
			}
		})
	}
}
