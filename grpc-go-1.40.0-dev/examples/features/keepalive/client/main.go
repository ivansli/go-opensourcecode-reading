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

// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/keepalive"
)

var addr = flag.String("addr", "localhost:50052", "the address to connect to")

// 客户端 keepalive
var kacp = keepalive.ClientParameters{
	// send pings every 10 seconds if there is no activity
	// 如果没有 activity， 则每隔 10s 发送一个 ping 包
	Time: 10 * time.Second,

	// wait 1 second for ping ack before considering the connection dead
	// 如果 ping ack 1s 之内未返回则认为连接已断开
	Timeout: time.Second,

	// send pings even without active streams
	// 如果没有 active 的 stream， 是否允许发送 ping
	PermitWithoutStream: true,
}

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*addr,
		grpc.WithInsecure(),
		grpc.WithKeepaliveParams(kacp), // 保持长连接参数配置
	)

	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fmt.Println("Performing unary request")
	res, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "keepalive demo"})
	if err != nil {
		log.Fatalf("unexpected error from UnaryEcho: %v", err)
	}
	fmt.Println("RPC response:", res)
	select {} // Block forever; run with GODEBUG=http2debug=2 to observe ping frames and GOAWAYs due to idleness.
}
