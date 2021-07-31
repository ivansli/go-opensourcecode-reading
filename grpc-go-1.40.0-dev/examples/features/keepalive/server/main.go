/*
 *
 * Copyright 2019 gRPC authors.
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
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var port = flag.Int("port", 50052, "port number")

// EnforcementPolicy用于设置服务器端保持连接的强制策略
// 服务器将关闭与违反此策略的客户端的连接。
var kaep = keepalive.EnforcementPolicy{
	// If a client pings more than once every 5 seconds, terminate the connection
	// 如果客户端两次 ping 的间隔小于 5s，则关闭连接
	MinTime: 5 * time.Second,

	// Allow pings even when there are no active streams
	//即使没有 active stream, 也允许 ping
	PermitWithoutStream: true,
}

// ServerParameters 用于设置服务器端的 keepalive 和 max-age 参数
var kasp = keepalive.ServerParameters{
	// If a client is idle for 15 seconds, send a GOAWAY
	// 如果一个 client 空闲超过 15s, 发送一个 GOAWAY, 为了防止同一时间发送大量 GOAWAY
	// 会在 15s 时间间隔上下浮动 15*10%, 即 15+1.5 或者 15-1.5
	MaxConnectionIdle: 15 * time.Second,

	// If any connection is alive for more than 30 seconds, send a GOAWAY
	// 如果任意连接存活时间超过 30s, 发送一个 GOAWAY
	MaxConnectionAge: 30 * time.Second,

	// Allow 5 seconds for pending RPCs to complete before forcibly closing connections
	// 在强制关闭连接之间, 允许有 5s 的时间完成 pending 的 rpc 请求
	MaxConnectionAgeGrace: 5 * time.Second,

	// Ping the client if it is idle for 5 seconds to ensure the connection is still active
	//如果一个 client 空闲超过 5s, 则发送一个 ping 请求
	Time: 5 * time.Second,

	// Wait 1 second for the ping ack before assuming the connection is dead
	//如果 ping 请求 1s 内未收到回复, 则认为该连接已断开
	Timeout: 1 * time.Second,
}

// server implements EchoServer.
type server struct {
	pb.UnimplementedEchoServer
}

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: req.Message}, nil
}

func main() {
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 参数中包含设置长连接的配置信息
	s := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))
	pb.RegisterEchoServer(s, &server{})

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
