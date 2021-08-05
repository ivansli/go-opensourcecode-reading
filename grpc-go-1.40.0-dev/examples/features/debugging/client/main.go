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

// Binary client is an example client.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	defaultName = "world"
)

func main() {
	////////////////////////////////////////////////////
	//  服务端逻辑
	////////////////////////////////////////////////////
	/***** Set up the server serving channelz service. *****/
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()
	s := grpc.NewServer()
	service.RegisterChannelzServiceToServer(s)
	go s.Serve(lis)
	defer s.Stop()

	////////////////////////////////////////////////////
	//  客户端逻辑
	////////////////////////////////////////////////////
	/***** Initialize manual resolver and Dial *****/
	r := manual.NewBuilderWithScheme("whatever")

	// Set up a connection to the server.
	// 设置连接配置
	conn, err := grpc.Dial(r.Scheme()+":///test.server",
		grpc.WithInsecure(),
		grpc.WithResolvers(r),

		// grpc.WithBalancerName、grpc.WithDefaultServiceConfig 二选一
		// 但是 grpc.WithBalancerName 将被弃用
		//grpc.WithBalancerName(roundrobin.Name),

		// https://github.com/grpc/grpc/blob/master/doc/service_config.md
		// grpc.WithDefaultServiceConfig
		//
		// {
		//  "loadBalancingConfig": [ { "round_robin": {} } ],
		//  "methodConfig": [
		//    {
		//      "name": [
		//        { "service": "foo", "method": "bar" },
		//        { "service": "baz" }
		//      ],
		//      "timeout": "1.000000001s"
		//    }
		//  ]
		// }
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
		// 从通道读取配置信息
		// 将被弃用
		grpc.WithServiceConfig(nil),

		grpc.WithTimeout(time.Second),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	// Manually provide resolved addresses for the target.
	// 手动为目标提供解析的地址
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: ":10001"}, {Addr: ":10002"}, {Addr: ":10003"}}})

	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	name := defaultName
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	/***** Make 100 SayHello RPCs *****/
	for i := 0; i < 100; i++ {
		// Setting a 150ms timeout on the RPC.
		// 设置请求超时时间
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
		if err != nil {
			log.Printf("could not greet: %v", err)
		} else {
			log.Printf("Greeting: %s", r.Message)
		}
	}

	/***** Wait for user exiting the program *****/
	// Unless you exit the program (e.g. CTRL+C), channelz data will be available for querying.
	// Users can take time to examine and learn about the info provided by channelz.
	select {}
}
