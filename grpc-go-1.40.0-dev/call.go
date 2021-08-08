/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc

import (
	"context"
)

// Invoke sends the RPC request on the wire and returns after response is
// received.  This is typically called by generated code.
//
// All errors returned by Invoke are compatible with the status package.
//
// 客户端请求服务端 都需要调用该方法
//
func (cc *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...CallOption) error {
	// allow interceptor to see all applicable call options, which means those
	// configured as defaults from dial option as well as per-call options
	//
	// combine 合并 CallOption
	//
	// CallOption 由向部分组成
	// 	一部分是在 Dial() 时设置
	// 	另一部分是在 请求具体方法时设置
	opts = combine(cc.dopts.callOptions, opts)

	//////////////////////////////////////////////
	// 一元拦截器存在，则把 invoke 封装进去
	// 含有拦截器的逻辑走这里
	// 多个拦截器存在的话 则逻辑类似 洋葱模型
	// 一层层包裹，真正的调用远端逻辑在最内层
	//////////////////////////////////////////////
	// 在 cc.dopts.unaryInt 拦截器中 会调用 invoke
	if cc.dopts.unaryInt != nil {
		return cc.dopts.unaryInt(ctx, method, args, reply, cc, invoke, opts...)
	}

	//////////////////////////////////////////////
	// 不含有拦截器的逻辑走这里
	//////////////////////////////////////////////
	return invoke(ctx, method, args, reply, cc, opts...)
}

func combine(o1 []CallOption, o2 []CallOption) []CallOption {
	// we don't use append because o1 could have extra capacity whose
	// elements would be overwritten, which could cause inadvertent
	// sharing (and race conditions) between concurrent calls
	if len(o1) == 0 {
		return o2
	} else if len(o2) == 0 {
		return o1
	}

	// 合并两部分 CallOption
	ret := make([]CallOption, len(o1)+len(o2))
	copy(ret, o1)
	copy(ret[len(o1):], o2)
	return ret
}

// Invoke sends the RPC request on the wire and returns after response is
// received.  This is typically called by generated code.
//
// DEPRECATED: Use ClientConn.Invoke instead.
func Invoke(ctx context.Context, method string, args, reply interface{}, cc *ClientConn, opts ...CallOption) error {
	return cc.Invoke(ctx, method, args, reply, opts...)
}

// 一元stream描述
// 表明： 即不是客户端流式stream 也不是服务端流式stream
var unaryStreamDesc = &StreamDesc{ServerStreams: false, ClientStreams: false}

func invoke(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error {
	///////////////////////////////////////////////////
	// newClientStream 创建新的 英语数据交互的 stream 流
	// TODO 追源码
	///////////////////////////////////////////////////

	// method 就是请求远端的方法
	// cc grpc.ClientConn
	//
	// 每一个方法都会生成一个 ClientStream
	cs, err := newClientStream(ctx, unaryStreamDesc, cc, method, opts...)
	if err != nil {
		return err
	}

	// req 入参
	// 发送请求参数
	// stream.go
	// func (cs *clientStream) SendMsg(m interface{}) (err error)
	if err := cs.SendMsg(req); err != nil {
		return err
	}

	// reply 远端返回结果
	return cs.RecvMsg(reply)
}
