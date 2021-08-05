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
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	ecpb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/resolver"
)

const (
	// 名称解析器
	exampleScheme      = "example"                  // 协议
	exampleServiceName = "resolver.example.grpc.io" // 服务名

	// 服务端地址
	backendAddr = "localhost:50051"
)

func callUnaryEcho(c ecpb.EchoClient, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r, err := c.UnaryEcho(ctx, &ecpb.EchoRequest{Message: message})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	fmt.Println(r.Message)
}

func makeRPCs(cc *grpc.ClientConn, n int) {
	hwc := ecpb.NewEchoClient(cc)
	for i := 0; i < n; i++ {
		callUnaryEcho(hwc, "this is examples/name_resolving")
	}
}

func main() {
	passthroughConn, err := grpc.Dial(
		// Dial to "passthrough:///localhost:50051"
		// 这里 '///' 三个斜线应该是  '//空/'，这里的空是因为没有设置 Authority 字段值
		// 协议 默认是 passthrough
		fmt.Sprintf("passthrough:///%s", backendAddr),
		grpc.WithInsecure(),
		grpc.WithBlock(),
	)

	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// 用完记得关闭哦
	defer passthroughConn.Close()

	fmt.Printf("--- calling helloworld.Greeter/SayHello to \"passthrough:///%s\"\n", backendAddr)
	makeRPCs(passthroughConn, 10)

	fmt.Println()

	// 第二个 客户端连接
	exampleConn, err := grpc.Dial(
		// 这里使用 'example:///resolver.example.grpc.io' 作为服务端地址
		// 这里的地址需要名称解析器进行解析到正确的ip地址
		//
		// 这里的 协议 example 已经在 init() 中注册到了明湖曾解析器
		// 所以明湖曾解析器可以正确的识别 example 协议，并获取对应的服务端地址
		fmt.Sprintf("%s:///%s", exampleScheme, exampleServiceName), // Dial to "example:///resolver.example.grpc.io"
		grpc.WithInsecure(),
		grpc.WithBlock(),
	)

	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// 用完 记得关闭哦
	defer exampleConn.Close()

	fmt.Printf("--- calling helloworld.Greeter/SayHello to \"%s:///%s\"\n", exampleScheme, exampleServiceName)
	makeRPCs(exampleConn, 10)
}

////////////////////////////////////////////////////////////////////
// 名词了解
// 名称解析器：把Dial中的服务名解析到具体的服务器地址列表
// 名称解析器构建器：用来构建名称解析器对象
//
// 名称解析器的使用方式
// 1. 首先需要在 "名称解析器构建器" 中生成 "名称解析器" 对象
// 2. 其次 需要把 "名称解析器构建器" 注册到 名称解析器构建器 的全局对象中
//
// "名称解析器构建器" 需要实现
// type Builder interface {
//	  // 构建一个名称解析器
//    Build(target Target, cc ClientConn, opts BuildOptions) (Resolver, error)
//    // 返回 名称解析器 对应的schema
//    Scheme() string
// }
///////////////////////////////////////////////////////////////////////

// Following is an example name resolver. It includes a
// ResolverBuilder(https://godoc.org/google.golang.org/grpc/resolver#Builder)
// and a Resolver(https://godoc.org/google.golang.org/grpc/resolver#Resolver).
//
// 下面是一个 名称解析器 的例子
//
// A ResolverBuilder is registered for a scheme (in this example, "example" is
// the scheme). When a ClientConn is created for this scheme, the
// ResolverBuilder will be picked to build a Resolver. Note that a new Resolver
// is built for each ClientConn. The Resolver will watch the updates for the
// target, and send updates to the ClientConn.
//
// 一个 ResolverBuilder(名称解析器构建器) 通过(scheme)协议来注册
// 当一个客户端通过 scheme 来连接服务端时，ResolverBuilder 将会找出对应的明湖曾解析器
// 一个 新的解析器 可以被各个 客户端连接 使用
// 解析器会检视目标地址的更新，并发送最新的地址给客户端连接
//
// exampleResolverBuilder is a
// ResolverBuilder(https://godoc.org/google.golang.org/grpc/resolver#Builder).

// 一个 名称解析器构建器 结构体
type exampleResolverBuilder struct{}

// 名称解析器构建器 构建 名称解析器
// 名称解析器构建器需要实现下面接口
// type Builder interface {
//    Build(target Target, cc ClientConn, opts BuildOptions) (Resolver, error)
//    Scheme() string
//}
//
// resolver.Target 名称解析器解析的目标地址
// resolver.BuildOptions 名称解析器构建器的选项
func (*exampleResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	// 一个名称解析器对象
	r := &exampleResolver{
		// 目标地址
		target: target,
		cc:     cc,
		// 服务端地址 以及 服务名的映射
		addrsStore: map[string][]string{
			exampleServiceName: {backendAddr, backendAddr},
		},
	}

	// 调用名称解析器的 start 方法
	r.start()

	// 返回名称解析器对象
	return r, nil
}

// 返回协议名
func (*exampleResolverBuilder) Scheme() string { return exampleScheme }

// exampleResolver 是一个名称解析器
// exampleResolver is a
// Resolver(https://godoc.org/google.golang.org/grpc/resolver#Resolver).
//
// 名称解析器必须实现以下接口
// type Resolver interface {
//    ResolveNow(ResolveNowOptions)
//    Close()
// }
//
type exampleResolver struct {
	// 在 Dial 中的目标地址对象
	target resolver.Target
	// 客户端连接 conn 对象
	// 最终会持有一个服务端地址列表，并且会被更新
	cc resolver.ClientConn

	// ！！！目标地址对象提供的地址列表
	addrsStore map[string][]string
}

// 更新服务端地址列表到客户端连接对象 resolver.ClientConn 中
// 通常还会 有一个新的协程定时更新列表信息到客户端conn中
func (r *exampleResolver) start() {
	// 通过服务名称 获取服务端地址列表
	addrStrs := r.addrsStore[r.target.Endpoint]

	// 把每一个地址封装到 resolver.Address 中
	addrs := make([]resolver.Address, len(addrStrs))
	for i, s := range addrStrs {
		addrs[i] = resolver.Address{Addr: s}
	}

	// 更新 客户端连接cc 的状态信息
	// 也就是更新服务端地址列表到 客户端连接中
	r.cc.UpdateState(resolver.State{Addresses: addrs})
}

func (*exampleResolver) ResolveNow(o resolver.ResolveNowOptions) {}
func (*exampleResolver) Close()                                  {}

// 注册 名称解析器
func init() {
	// Register the example ResolverBuilder. This is usually done in a package's
	// init() function.
	//
	// 注册名称解析器的构建器
	// 通过在 init() 方法中进行注册
	resolver.Register(&exampleResolverBuilder{})
}
