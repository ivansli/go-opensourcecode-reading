// 服务发现使用 etcd 名称解析
package main

import (
	"context"
	"fmt"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log"
	"time"

	"google.golang.org/grpc"
	ecpb "google.golang.org/grpc/examples/features/proto/echo"
)

const (
	// etcd resolver 负责的 scheme 类型
	EtcdScheme      = "etcd"       //scheme
	EtcdServiceName = "helloworld" // 服务名

	defaultFreq = time.Minute * 30
)

func callUnaryEcho(c ecpb.EchoClient, message string) {
	// 带有超时控制
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r, err := c.UnaryEcho(ctx, &ecpb.EchoRequest{Message: message})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	fmt.Println(r.Message)
}

// 多次请求rpc，分别负载到不停的服务端地址上
func makeRPCs(cc *grpc.ClientConn, n int) {
	hwc := ecpb.NewEchoClient(cc)
	for i := 0; i < n; i++ {
		callUnaryEcho(hwc, "this is examples/name_resolving")
	}
}

func main() {
	// etcd 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"127.0.0.1:2379"},
	})
	if err != nil {
		panic(err)
	}

	etcdResolverConn, err := grpc.Dial(
		// 这里使用 'example:///resolver.example.grpc.io' 作为服务端地址
		// 这里的地址需要名称解析器进行解析到正确的ip地址
		// 这里的 协议 example 已经在 init() 中注册到了明湖曾解析器
		// 所以名称解析器可以正确的识别 example 协议，并获取对应的服务端地址

		// EtcdScheme 用于找到名称解析器的构造器
		// EtcdServiceName 是服务端进行服务注册时 设置的 etcd key 前缀
		fmt.Sprintf("%s:///%s", EtcdScheme, EtcdServiceName), // Dial to "example:///resolver.example.grpc.io"
		grpc.WithInsecure(),
		grpc.WithBlock(),

		// !!!!
		// 注册名称解析器的构造器
		grpc.WithResolvers(NewBuilder(cli)),
		// 指定负载均衡器
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)

	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// 用完 记得关闭哦
	defer etcdResolverConn.Close()

	fmt.Printf("--- calling helloworld.Greeter/SayHello to \"%s:///%s\"\n", EtcdScheme, EtcdServiceName)
	makeRPCs(etcdResolverConn, 10)
}


