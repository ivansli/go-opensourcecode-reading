/*
 *
 * Copyright 2018 gRPC authors.
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

// Binary server is an example server.
package main

import (
	"context"
	"fmt"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/proxy/grpcproxy"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

const addr = "localhost:50051"

type ecServer struct {
	pb.UnimplementedEchoServer
	addr string
}

func (s *ecServer) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: fmt.Sprintf("%s (from %s)", req.Message, s.addr)}, nil
}

// 注册服务到etcd中
func service(port string) error {
	etcdv3cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://localhost:2379"},
		DialTimeout: 60 * time.Second,
	})
	if err != nil {
		return err
	}
	defer etcdv3cli.Close()

	// 服务地址
	target := fmt.Sprintf("/etcdv3://go-program/grpc/%s", "service")
	// ！！！
	// 服务端注册服务信息到etcd中
	//
	// Register registers itself as a grpc-proxy server by writing prefixed-key
	// with session of specified TTL (in seconds). The returned channel is closed
	// when the client's context is canceled.
	//
	//
	// func Register(c *clientv3.Client, prefix string, addr string, ttl int) <-chan struct{}
	//
	// 把当前服务器的地址 "localhost:50051" 注册到 etcd 的 target 路径中
	// 并设置ttl为60秒
	grpcproxy.Register(etcdv3cli, target, ":"+port, 60)

	return nil
	//return http.ListenAndServe(":"+port, nil)
}

func main() {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &ecServer{addr: addr})

	// ！！！重要
	// 注册服务到etcd
	service(addr)

	log.Printf("serving on %s\n", addr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
