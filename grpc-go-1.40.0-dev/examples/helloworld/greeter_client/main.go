/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package main implements a client for Greeter service.
package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/resolver/passthrough"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	address     = "localhost:50051"
	defaultName = "world"
)

// unaryInterceptor is an example unary interceptor.
//
// 一元拦截器
func unaryInterceptor1(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	start := time.Now()

	// 请求服务端
	err := invoker(ctx, method, req, reply, cc, opts...)

	end := time.Now()
	fmt.Printf("unaryInterceptor1 RPC: %s, start time: %s, end time: %s\n", method, start.Format("Basic"), end.Format(time.RFC3339Nano))
	return err
}

// unaryInterceptor is an example unary interceptor.
//
// 一元拦截器
func unaryInterceptor2(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	start := time.Now()

	// 请求服务端
	err := invoker(ctx, method, req, reply, cc, opts...)

	end := time.Now()
	fmt.Printf("unaryInterceptor2 RPC: %s, start time: %s, end time: %s\n", method, start.Format("Basic"), end.Format(time.RFC3339Nano))
	return err
}

// unaryInterceptor is an example unary interceptor.
//
// 一元拦截器
func unaryInterceptor3(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	start := time.Now()

	// 请求服务端
	err := invoker(ctx, method, req, reply, cc, opts...)

	end := time.Now()
	fmt.Printf("unaryInterceptor3 RPC: %s, start time: %s, end time: %s\n", method, start.Format("Basic"), end.Format(time.RFC3339Nano))
	return err
}

func main() {
	// Set up a connection to the server.
	//
	// 建立连接
	// grpc.WithInsecure() 表示不使用tsl
	// grpc.WithBlock() 表示在连接正式建立之前一直处于阻塞状态，直到连接建立成功
	//
	// grpc-go/clientconn.go
	conn, err := grpc.Dial(address,
		grpc.WithInsecure(),
		grpc.WithBlock(),
		//grpc.WithTimeout(time.Second),  // 将会被弃用,使用 grpc.DialContext() 替代

		// 拦截器设置
		// 单独设置的在合并时排在第一个，但 是最后执行
		// 一次设置单独的拦截器
		grpc.WithUnaryInterceptor(unaryInterceptor3),
		// 一次设置多个拦截器
		grpc.WithChainUnaryInterceptor(unaryInterceptor1, unaryInterceptor2),

		// ！！！！
		// 注册 名称解析器的构造器
		// 或者 使用  resolver.Register(passthrough.NewBuilder())
		// passthrough.NewBuilder() 也可以是自己实现的构造器对象，例如 etcd等
		grpc.WithResolvers(passthrough.NewBuilder()),
		// 设置负载均衡器的构造器
		// 在加载 roundrobin 文件时
		// 已经通过 init调用 balancer.Register(newBuilder())，进行了构造器注册
		grpc.WithBalancerName(roundrobin.Name),

		// 将被弃用
		//grpc.WithDialer(func(s string, duration time.Duration) (net.Conn, error) {}),
	)

	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// 关闭连接，十分重要
	defer conn.Close()

	// 把连接conn封装到服务对象Greeter中
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	name := defaultName
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	// 创建请求超时时间ctx
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	// 注意defer
	defer cancel()

	// 请求服务接口 SayHello
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}

	log.Printf("Greeting: %s", r.GetMessage())
}
