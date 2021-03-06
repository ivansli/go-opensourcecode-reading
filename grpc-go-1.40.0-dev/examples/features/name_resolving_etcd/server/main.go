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
	"flag"
	"fmt"
	"github.com/pkg/errors"
	clientv3 "go.etcd.io/etcd/client/v3"
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

const (
	addr = "localhost"

	EtcdScheme      = "etcd"
	EtcdServiceName = "helloworld"
)

type ecServer struct {
	pb.UnimplementedEchoServer
	addr string
}

func (s *ecServer) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	fmt.Println("client request:", req.String())
	return &pb.EchoResponse{Message: fmt.Sprintf("%s (from %s)", req.Message, s.addr)}, nil
}

func main() {
	// 服务端端口, 可以在执行时自定义端口
	// go run main.go -port=5002
	port := flag.String("port", "5001", "server port")
	flag.Parse()

	// 服务端地址
	address := addr + ":" + (*port)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &ecServer{addr: address})

	ctx := context.Background()

	// etcd 客户端
	cli, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"127.0.0.1:2379"},
	})
	if err != nil {
		panic(err)
	}
	// 服务注册
	go Register(ctx, cli, EtcdServiceName, address)

	log.Printf("serving on %s\n", address)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// 服务端注册 到 etcd
func Register(ctx context.Context, client *clientv3.Client, service, self string) error {
	resp, err := client.Grant(ctx, 2)
	if err != nil {
		return errors.Wrap(err, "etcd grant")
	}

	// 把服务端地址self 注册到etcd中
	_, err = client.Put(ctx, strings.Join([]string{service, self}, "/"), self, clientv3.WithLease(resp.ID))
	if err != nil {
		return errors.Wrap(err, "etcd put")
	}

	// 保持长连接
	// respCh 需要消耗, 不然会有 warning
	respCh, err := client.KeepAlive(ctx, resp.ID)
	if err != nil {
		return errors.Wrap(err, "etcd keep alive")
	}

	// 一定要有这个
	// respCh 需要消耗, 不然会有 warning
	for {
		select {
		case <-ctx.Done(): // 上下文 取消或超时
			return nil
		case <-respCh:
			//fmt.Printf("d := <-respCh\n")
		}
	}
}
