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

// Package main implements a server for Greeter service.
package main

import (
	"context"
	channelz "github.com/rantav/go-grpc-channelz"
	"google.golang.org/grpc"
	channelzservice "google.golang.org/grpc/channelz/service"
	"log"
	"net"
	"net/http"

	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	port     = ":8000"
	web_port = ":8081"
)

// server is used to implement helloworld.GreeterServer.
//
// 实现 proto中定义的服务接口 的结构体
type server struct {
	// protoc自动生成的 实现 proto中定义的服务接口 的结构体
	// 但是在实现中没有完全实现具体逻辑，返回给客户端 codes.Unimplemented
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received: %v", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

// 参考
// https://github.com/rantav/go-grpc-channelz
// https://github.com/grpc/proposal/blob/master/A14-channelz.md
//
// 博客
// https://blog.csdn.net/RA681t58CJxsgCkJ31/article/details/104079070
// https://blog.csdn.net/u013536232/article/details/108556544
// 注册 channelZ 服务
//
//
// Channelz是一种用于内省到GRPC频道的GRPC规范
// GRPC的频道代表连接和套接字
// Channelz将内省提供到当前的活动GRPC连接中，包括传入和传出连接，可以在这里找到完整的规范
func channelZ(s *grpc.Server) {
	// Register the channelz handler
	//http.Handle("/", channelz.CreateHandler("/foo", grpcBindAddress))
	//
	// 注意：在 channelz.CreateHandler 中 createRouter(prefix, handler) 注册了 若干接口
	http.Handle("/", channelz.CreateHandler("/foo", port))

	// Register the channelz gRPC service to grpcServer so that we can query it for this service.
	// Channelz被实现为gRPC服务。默认情况下，此服务是关闭的，因此须这样打开它
	channelzservice.RegisterChannelzServiceToServer(s)
	//channelzservice.RegisterChannelzServiceToServer(grpcServer)

	// Listen and serve HTTP for the default serve mux
	// web_port 为对外提供的http服务
	adminListener, err := net.Listen("tcp", web_port)
	if err != nil {
		log.Fatal(err)
	}

	go http.Serve(adminListener, nil)
}

func main() {
	// 建立监听
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 生成服务对象
	grpcServer := grpc.NewServer()
	// &server{} 注册服务 到 s
	pb.RegisterGreeterServer(grpcServer, &server{})

	// channelZ
	channelZ(grpcServer)

	log.Printf("server listening at %v", lis.Addr())

	// 开始对外提供服务
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
