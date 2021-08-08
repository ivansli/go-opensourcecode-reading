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
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	addr = flag.String("addr", "localhost:50052", "the address to connect to")
	// see https://github.com/grpc/grpc/blob/master/doc/service_config.md to know more about service config

	// 参考 https://blog.csdn.net/DAGU131/article/details/106122895
	// service config 是和服务名称绑定的
	// 客户端名称解析插件解析服务名会返回解析的地址和 service config
	// 名称解析返回给 grpc client 是 json 格式的 service config

	// 重试策略
	// retryPolicy 最多执行四次 RPC 请求，一个原始请求，三个重试请求
	// 并且只有状态码为 UNAVAILABLE 时才重试
	// 最大重试次数 maxAttempts
	// 		指定一次RPC 调用中最多的请求次数，包括第一次请求
	// 重试状态码 retryableStatusCode
	// 		当RPC调用返回非 OK 响应，会根据 retryableStatusCode 来判断是否进行重试
	// 指数退避
	// 		在进行下一次重试请求前，会计算需要等待的时间
	//		第一次重试间隔是 random(0, initialBackoff)
	//		第 n 次的重试间隔为 random(0, min( initialBackoff*backoffMultiplier**(n-1) , maxBackoff))
	//
	// retryPolicy 参数要求
	//		maxAttempts 必须是大于 1 的整数，对于大于5的值会被视为5
	//		initialBackoff 和 maxBackoff 必须指定，并且必须具有大于0
	//		backoffMultiplier 必须指定，并且大于零
	//		retryableStatusCodes 必须制定为状态码的数据，不能为空
	//		并且没有状态码必须是有效的 gPRC 状态码，可以是整数形式
	//		并且不区分大小写 ([14], [“UNAVAILABLE”], [“unavailable”)
	retryPolicy = `{
		"methodConfig": [{
		  "name": [{"service": "grpc.examples.echo.Echo"}],
		  "waitForReady": true,
		  "retryPolicy": {
			  "MaxAttempts": 4,
			  "InitialBackoff": ".01s",
			  "MaxBackoff": ".01s",
			  "BackoffMultiplier": 1.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`
)

// use grpc.WithDefaultServiceConfig() to set service config
func retryDial() (*grpc.ClientConn, error) {
	return grpc.Dial(*addr, grpc.WithInsecure(), grpc.WithDefaultServiceConfig(retryPolicy))
}

func main() {
	flag.Parse()

	// Set up a connection to the server.
	conn, err := retryDial()
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer func() {
		if e := conn.Close(); e != nil {
			log.Printf("failed to close connection: %s", e)
		}
	}()

	c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	reply, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "Try and Success"})
	if err != nil {
		log.Fatalf("UnaryEcho error: %v", err)
	}
	log.Printf("UnaryEcho reply: %v", reply)
}
