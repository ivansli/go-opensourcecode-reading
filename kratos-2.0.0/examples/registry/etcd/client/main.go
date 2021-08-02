package main

import (
	"context"
	"log"

	"github.com/go-kratos/etcd/registry"
	"github.com/go-kratos/kratos/examples/helloworld/helloworld"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"127.0.0.1:2379"},
	})
	if err != nil {
		panic(err)
	}
	// 提供服务发现的服务对象，这里是 etcd
	r := registry.New(cli)

	conn, err := grpc.DialInsecure(
		context.Background(),
		// 服务地址
		grpc.WithEndpoint("discovery:///helloworld"),
		// 服务发现
		grpc.WithDiscovery(r),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := helloworld.NewGreeterClient(conn)
	reply, err := client.SayHello(context.Background(), &helloworld.HelloRequest{Name: "kratos"})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("[grpc] SayHello %+v\n", reply)
}
