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
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpcutil"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"

	// 注册的若干个 名称解析器的构造器
	_ "google.golang.org/grpc/balancer/roundrobin"           // To register roundrobin.
	_ "google.golang.org/grpc/internal/resolver/dns"         // To register dns resolver.
	_ "google.golang.org/grpc/internal/resolver/passthrough" // To register passthrough resolver.
	_ "google.golang.org/grpc/internal/resolver/unix"        // To register unix resolver.
)

const (
	// minimum time to give a connection to complete
	minConnectTimeout = 20 * time.Second
	// must match grpclbName in grpclb/grpclb.go
	grpclbName = "grpclb"
)

var (
	// ErrClientConnClosing indicates that the operation is illegal because
	// the ClientConn is closing.
	//
	// Deprecated: this error should not be relied upon by users; use the status
	// code of Canceled instead.
	ErrClientConnClosing = status.Error(codes.Canceled, "grpc: the client connection is closing")
	// errConnDrain indicates that the connection starts to be drained and does not accept any new RPCs.
	errConnDrain = errors.New("grpc: the connection is drained")
	// errConnClosing indicates that the connection is closing.
	errConnClosing = errors.New("grpc: the connection is closing")
	// invalidDefaultServiceConfigErrPrefix is used to prefix the json parsing error for the default
	// service config.
	invalidDefaultServiceConfigErrPrefix = "grpc: the provided default service config is invalid"
)

// The following errors are returned from Dial and DialContext
var (
	// errNoTransportSecurity indicates that there is no transport security
	// being set for ClientConn. Users should either set one or explicitly
	// call WithInsecure DialOption to disable security.
	errNoTransportSecurity = errors.New("grpc: no transport security set (use grpc.WithInsecure() explicitly or set credentials)")
	// errTransportCredsAndBundle indicates that creds bundle is used together
	// with other individual Transport Credentials.
	errTransportCredsAndBundle = errors.New("grpc: credentials.Bundle may not be used with individual TransportCredentials")
	// errTransportCredentialsMissing indicates that users want to transmit security
	// information (e.g., OAuth2 token) which requires secure connection on an insecure
	// connection.
	errTransportCredentialsMissing = errors.New("grpc: the credentials require transport level security (use grpc.WithTransportCredentials() to set)")
	// errCredentialsConflict indicates that grpc.WithTransportCredentials()
	// and grpc.WithInsecure() are both called for a connection.
	errCredentialsConflict = errors.New("grpc: transport credentials are set for an insecure connection (grpc.WithTransportCredentials() and grpc.WithInsecure() are both called)")
)

const (
	defaultClientMaxReceiveMessageSize = 1024 * 1024 * 4
	defaultClientMaxSendMessageSize    = math.MaxInt32
	// http2IOBufSize specifies the buffer size for sending frames.
	defaultWriteBufSize = 32 * 1024
	defaultReadBufSize  = 32 * 1024
)

// Dial creates a client connection to the given target.
// 创建一个目标地址的客户端连接 ClientConn
func Dial(target string, opts ...DialOption) (*ClientConn, error) {
	///////////////////////////////////////////////////////////////////////
	// 注意这里的 context.Background() 不带有超时时间
	// 如果需要设置连接超时时间，则需要直接调用 DialContext，并且第一个参数设置为有超时时间的ctx
	///////////////////////////////////////////////////////////////////////
	return DialContext(context.Background(), target, opts...)
}

type defaultConfigSelector struct {
	sc *ServiceConfig
}

func (dcs *defaultConfigSelector) SelectConfig(rpcInfo iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
	return &iresolver.RPCConfig{
		Context:      rpcInfo.Context,
		MethodConfig: getMethodConfig(dcs.sc, rpcInfo.Method),
	}, nil
}

// DialContext creates a client connection to the given target. By default, it's
// a non-blocking dial (the function won't wait for connections to be
// established, and connecting happens in the background). To make it a blocking
// dial, use WithBlock() dial option.
//
// In the non-blocking case, the ctx does not act against the connection. It
// only controls the setup steps.
//
// In the blocking case, ctx can be used to cancel or expire the pending
// connection. Once this function returns, the cancellation and expiration of
// ctx will be noop. Users should call ClientConn.Close to terminate all the
// pending operations after this function returns.
//
// The target name syntax is defined in
// https://github.com/grpc/grpc/blob/master/doc/naming.md.
// e.g. to use dns resolver, a "dns:///" prefix should be applied to the target.
//
// 通过给的target客户端地址创建一个客户端连接，默认使用非阻塞dial
// 如果是阻塞创建连接，则使用 WithBlock()选项
// 在非阻塞创建连接中，ctx是无效的
//
// 在阻塞的情况下，可以使用ctx取消挂起的连接或使其失效
// 一旦这个函数返回，取消和过期的ctx将是noop
// 用户应该调用ClientConn.Close用于在函数返回后终止所有挂起的操作
func DialContext(ctx context.Context, target string, opts ...DialOption) (conn *ClientConn, err error) {
	// 客户端连接
	cc := &ClientConn{
		// 请求的目标地址
		target: target,

		// 管理conn的连接状态信息
		// cs 指 connectivity State
		csMgr: &connectivityStateManager{},

		conns: make(map[*addrConn]struct{}),

		// Dial() 设置的 Options 信息默认值
		dopts: defaultDialOptions(),

		// pickerWrapper是balancer.Picker的包装器
		blockingpicker: newPickerWrapper(),

		// channelZ 监控信息
		czData:            new(channelzData),
		firstResolveEvent: grpcsync.NewEvent(),
	}

	// 重试相关信息 先 store一个空值
	// cc.retryThrottler 为 atomic.Value 类型
	// 重试节流阀
	cc.retryThrottler.Store((*retryThrottler)(nil))

	// 设置默认的 resolver.ConfigSelector
	cc.safeConfigSelector.UpdateConfigSelector(&defaultConfigSelector{nil})

	// 设置客户端 ClientConn 的上下文
	cc.ctx, cc.cancel = context.WithCancel(context.Background())

	// 调用 Dial() 方法时设置的信息都配置到 cc.dopts 结构体上
	for _, opt := range opts {
		opt.apply(&cc.dopts)
	}

	// 拦截器设置
	// 如果存在多个拦截器，则合并成一个拦截器调用链
	chainUnaryClientInterceptors(cc)
	chainStreamClientInterceptors(cc)

	// 延迟执行
	defer func() {
		// 如果有错误发生，则置空 cc
		if err != nil {
			cc.Close()
		}
	}()

	// channelz 是 grpc 内部的一些埋点监控性质的信息
	// 大体上是一个异步的 AddTraceEvent 然后汇聚数值
	//
	// gRPC 提供了 Channelz 用于对外提供服务的数据，用于调试、监控等
	// 根据服务的角色不同，可以提供的数据有：
	// 	  服务端: Servers, Server, ServerSockets, Socket
	// 	  客户端: TopChannels, Channel, Subchannel
	//
	// channelz 开启，则注册一些信息
	// !!! 阅读代码可以忽略这些 !!!
	if channelz.IsOn() {
		if cc.dopts.channelzParentID != 0 {
			cc.channelzID = channelz.RegisterChannel(&channelzChannel{cc}, cc.dopts.channelzParentID, target)
			channelz.AddTraceEvent(logger, cc.channelzID, 0, &channelz.TraceEventDesc{
				Desc:     "Channel Created",
				Severity: channelz.CtInfo,
				Parent: &channelz.TraceEventDesc{
					Desc:     fmt.Sprintf("Nested Channel(id:%d) created", cc.channelzID),
					Severity: channelz.CtInfo,
				},
			})
		} else {
			cc.channelzID = channelz.RegisterChannel(&channelzChannel{cc}, 0, target)
			channelz.Info(logger, cc.channelzID, "Channel Created")
		}
		cc.csMgr.channelzID = cc.channelzID
	}

	// dialOption 没设置 insecure, 即: 没设置 grpc.WithInsecure()
	if !cc.dopts.insecure {
		// 开启TLS
		// 对https证书，密钥做参数校验
		if cc.dopts.copts.TransportCredentials == nil && cc.dopts.copts.CredsBundle == nil {
			return nil, errNoTransportSecurity
		}
		if cc.dopts.copts.TransportCredentials != nil && cc.dopts.copts.CredsBundle != nil {
			return nil, errTransportCredsAndBundle
		}
	} else {
		// grpc.WithInsecure() 来控制是否开启安全传输
		// 没有开启安全传输
		if cc.dopts.copts.TransportCredentials != nil || cc.dopts.copts.CredsBundle != nil {
			return nil, errCredentialsConflict
		}

		// 验证自定义校验中是否设置了需要 TLS
		// 如果设置了，则报错
		for _, cd := range cc.dopts.copts.PerRPCCredentials {
			//  cd.RequireTransportSecurity() 等于 true， 需要使用安全连接
			if cd.RequireTransportSecurity() {
				return nil, errTransportCredentialsMissing
			}
		}
	}

	// 通过json文本形式设置ServiceConfig
	// 在 Dial() 中通过 grpc.WithDefaultServiceConfig("") 来设置
	// 例如：
	// grpc.WithDefaultServiceConfig(fmt.Sprintf(`{ "loadBalancingConfig": [{"%v": {}}] }`, roundrobin.Name))
	//
	//  例子：
	//    features/debugging/client/main.go
	//    features/retry/client/main.go
	if cc.dopts.defaultServiceConfigRawJSON != nil {
		// TODO （read code）
		//  解析json字符串
		scpr := parseServiceConfig(*cc.dopts.defaultServiceConfigRawJSON)
		if scpr.Err != nil {
			return nil, fmt.Errorf("%s: %v", invalidDefaultServiceConfigErrPrefix, scpr.Err)
		}

		// defaultServiceConfig 跟 负载均衡等相关
		cc.dopts.defaultServiceConfig, _ = scpr.Config.(*ServiceConfig)
	}

	// KeepaliveParams
	// params := keepalive.ClientParameters{
	//			Time:                c.cfg.DialKeepAliveTime,
	//			Timeout:             c.cfg.DialKeepAliveTimeout,
	//			PermitWithoutStream: c.cfg.PermitWithoutStream,
	//		}
	//
	// 在 Dial() 中 通过 grpc.WithKeepaliveParams(params) 设置
	cc.mkp = cc.dopts.copts.KeepaliveParams

	// 请求头中 ua 设置
	// 添加 "grpc-go/版本号" 标识
	if cc.dopts.copts.UserAgent != "" {
		cc.dopts.copts.UserAgent += " " + grpcUA
	} else {
		cc.dopts.copts.UserAgent = grpcUA
	}

	// 客户端超时控制是通过 Context.WithTimeout 进行控制的
	// 其超时时长可以通过 grpc.WithTimeout(time.Duration) 控制
	// 未来 grpc.WithTimeout(time.Duration) 将被弃用
	//
	// 连接超时时间

	// 这里存在一个优先级问题：
	// 1. 如果 dialOption 设置了超时时间，则根据超时时间封装一个ctx
	// 2. 否则，使用 DialContext 第一个参数
	if cc.dopts.timeout > 0 {
		var cancel context.CancelFunc

		// 覆盖 DialContext() 第一个参数 ctx 的值
		ctx, cancel = context.WithTimeout(ctx, cc.dopts.timeout)
		defer cancel()
	}

	// 延迟执行
	// 在这里控制连接超时时间，
	// 但如果没设置 grpc.WithBlock() 这里的超时控制基本没用
	defer func() {
		select {
		// <-ctx.Done() 说明上下文已经超时取消了
		// 所以置空创建的连接 conn
		case <-ctx.Done():
			switch {
			case ctx.Err() == err:
				conn = nil
			case err == nil || !cc.dopts.returnLastError:
				conn, err = nil, ctx.Err()
			default:
				conn, err = nil, fmt.Errorf("%v: %v", ctx.Err(), err)
			}

		default:
		}
	}()

	// 从 scChan通道中 监听接收 serviceConfig 信息
	// 在Dial() 中通过 grpc.WithServiceConfig(chan), 使用
	// 将被弃用
	scSet := false
	if cc.dopts.scChan != nil {
		// Try to get an initial service config.
		select {
		case sc, ok := <-cc.dopts.scChan:
			if ok {
				cc.sc = &sc
				cc.safeConfigSelector.UpdateConfigSelector(&defaultConfigSelector{&sc})
				scSet = true
			}
		default:
		}
	}

	// 默认取指数退避
	if cc.dopts.bs == nil {
		cc.dopts.bs = backoff.DefaultExponential
	}

	// Determine the resolver to use.
	//
	// "unknown_scheme://authority/endpoint"
	// 确定 使用的 resolver
	//
	// cc.parsedTarget 结构为：
	// type Target struct {
	//    Scheme    string
	//    Authority string
	//    Endpoint  string
	// }

	// 解析调用的目标地址信息 包含：Scheme、Authority、Endpoint
	cc.parsedTarget = grpcutil.ParseTarget(cc.target, cc.dopts.copts.Dialer != nil)

	// 记录日志
	channelz.Infof(logger, cc.channelzID, "parsed scheme: %q", cc.parsedTarget.Scheme)

	// getResolver 优先从 DialOption 中读取
	// 其次中全局读取，DialOption 可以通过 grpc.WithResolvers() 设置
	// 全局的可以通过 resolver.Register(Builder) 注册,用法可参考 resolver/dns/dns_resolver.go

	// 注册  balancer
	// 	_ "google.golang.org/grpc/balancer/roundrobin"           // To register roundrobin.

	// 注册 若干 resolver
	//	_ "google.golang.org/grpc/internal/resolver/dns"         // To register dns resolver.
	//	_ "google.golang.org/grpc/internal/resolver/passthrough" // To register passthrough resolver.
	//	_ "google.golang.org/grpc/internal/resolver/unix"        // To register unix resolver.

	// 在本文件引入对应包的时候聚进行了初始化操作，其中包括 passthrough 注册到全局存储 resolver 对象的变量中
	//
	// 根据 cc.target 解析出来的 Scheme 来查找 名称解析器构建器(resolver.Builder)对象
	//
	// resolver.Builder 一般在init()中先于连接建立前注册到构建器的包全局对象中

	// 获取 名称解析器的构建器
	resolverBuilder := cc.getResolver(cc.parsedTarget.Scheme)

	// 名称解析器构建器对象 为空，则设置为默认的 passthrough
	if resolverBuilder == nil {
		// 记录日志
		channelz.Infof(logger, cc.channelzID, "scheme %q not registered, fallback to default scheme", cc.parsedTarget.Scheme)

		// If resolver builder is still nil, the parsed target's scheme is
		// not registered. Fallback to default resolver and set Endpoint to
		// the original target.
		//
		// 如果解析器构建器仍然为空，则解析目标的方案没有注册
		// 回退到默认解析器，并将端点设置为原始目标

		// 此时使用传递的 target 封装一个 resolver.Target 对象
		// 为空则尝试使用默认的 "passthrough" 名称解析器构建器对象
		//
		// 本文件头部已引入 passthrough 文件，在文件中 init() 方法 注册了 passthrough 构造器
		cc.parsedTarget = resolver.Target{
			// func SetDefaultScheme(scheme string)  用来设置默认 scheme
			// 例如：resolver.SetDefaultScheme("dns")
			Scheme:   resolver.GetDefaultScheme(), //  "passthrough"
			Endpoint: target,
		}

		// 走到这里会成功，因为文件头部引入了若干个包已经进行了初始化操作，已经进行了若干个注册
		// "passthrough"
		resolverBuilder = cc.getResolver(cc.parsedTarget.Scheme)

		// 还是没找到（则说明没有引入对应的包进行初始化），则返回错误
		if resolverBuilder == nil {
			return nil, fmt.Errorf("could not get resolver for default scheme: %q", cc.parsedTarget.Scheme)
		}
	}

	// 解析请求地址的 authority(认证)
	creds := cc.dopts.copts.TransportCredentials
	if creds != nil && creds.Info().ServerName != "" {
		cc.authority = creds.Info().ServerName
	} else if cc.dopts.insecure && cc.dopts.authority != "" {
		cc.authority = cc.dopts.authority
	} else if strings.HasPrefix(cc.target, "unix:") || strings.HasPrefix(cc.target, "unix-abstract:") {
		cc.authority = "localhost"
	} else if strings.HasPrefix(cc.parsedTarget.Endpoint, ":") {
		cc.authority = "localhost" + cc.parsedTarget.Endpoint
	} else {
		// Use endpoint from "scheme://authority/endpoint" as the default
		// authority for ClientConn.
		cc.authority = cc.parsedTarget.Endpoint
	}

	// 注意：scChan 不为空 并且 serveconfig 没配置，则会阻塞，直到 接收到数据
	// 阻塞等待 scChan
	//
	// 如果 scChan 存在，上面 scChan 由于某些原因没有接收到 serviceconf 数据
	// 就在这里一直等待到接收到数据 或者 超时、取消
	if cc.dopts.scChan != nil && !scSet {
		// Blocking wait for the initial service config.
		// 阻塞直到 service config 初始化
		select {
		case sc, ok := <-cc.dopts.scChan:
			if ok {
				cc.sc = &sc
				cc.safeConfigSelector.UpdateConfigSelector(&defaultConfigSelector{&sc})
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// scChan 存在，则启动新的协程监听其变化
	if cc.dopts.scChan != nil {
		// TODO 注意：新的 goroutine
		//  启动新的协程来监听 sc 变化
		go cc.scWatcher()
	}

	var credsClone credentials.TransportCredentials
	if creds := cc.dopts.copts.TransportCredentials; creds != nil {
		credsClone = creds.Clone()
	}

	// 负载均衡器的构造器 配置信息初始化
	// 初始化 负载均衡器的构造器 options
	cc.balancerBuildOpts = balancer.BuildOptions{
		DialCreds:        credsClone,
		CredsBundle:      cc.dopts.copts.CredsBundle,
		Dialer:           cc.dopts.copts.Dialer,    // ?
		CustomUserAgent:  cc.dopts.copts.UserAgent, // ua
		ChannelzParentID: cc.channelzID,            // channelz
		Target:           cc.parsedTarget,          // 解析的目标地址
	}

	///////////////////////////////////////////////////////////////////////
	// TODO (追源码)
	// Build the resolver.
	// ！！！ 核心
	// 在 newCCResolverWrapper 中 进行异步创建连接（使用其他goroutine）
	//
	// rWrapper 对 名称解析器对象、解析出的服务端列表、客户端连接对象 的封装
	///////////////////////////////////////////////////////////////////////

	// rWrapper 名称解析器的包装对象
	// 在随后的 invoke 发起针对具体方法的调用时要使用
	rWrapper, err := newCCResolverWrapper(cc, resolverBuilder)
	if err != nil {
		return nil, fmt.Errorf("failed to build resolver: %v", err)
	}

	cc.mu.Lock()
	cc.resolverWrapper = rWrapper //名称解析器的包装对象
	cc.mu.Unlock()

	// A blocking dial blocks until the clientConn is ready.
	//
	// 在 DialContext 文档注释中已经说明，block会阻塞dial过程，
	// 直到连接状态变为 Ready 或者遇到错误，默认是非阻塞的（即在后台建立连接操作）。
	// 开发者可以通过 grpc.WithBlock() 来控制阻塞
	//
	// 如果需要等待连接建立之后，才能发起请求的话，必须走这里
	if cc.dopts.block {
		for {
			// 获取连接状态
			s := cc.GetState()

			// 连接就绪 connectivity.Ready，则跳出
			if s == connectivity.Ready {
				break
			} else if cc.dopts.copts.FailOnNonTempDialError && s == connectivity.TransientFailure {
				if err = cc.connectionError(); err != nil {
					terr, ok := err.(interface {
						Temporary() bool
					})
					if ok && !terr.Temporary() {
						return nil, err
					}
				}
			}

			// 阻塞
			// 直到状态发送变化 (与当前的s不同就视为发生变化) 或者 超时或者取消了
			if !cc.WaitForStateChange(ctx, s) {
				// ctx got timeout or canceled.
				// 获取连接的错误信息
				if err = cc.connectionError(); err != nil && cc.dopts.returnLastError {
					return nil, err
				}

				// 这里返回 说明 连接还没创建好，就已经超时或者取消了
				return nil, ctx.Err()
			}
		}
	}

	return cc, nil
}

// chainUnaryClientInterceptors chains all unary client interceptors into one.
//
// 合并一元拦截器
func chainUnaryClientInterceptors(cc *ClientConn) {
	// 多个拦截器组成的切片
	interceptors := cc.dopts.chainUnaryInts

	// Prepend dopts.unaryInt to the chaining interceptors if it exists, since unaryInt will
	// be executed before any other chained interceptors.
	//
	// 合并多个拦截器， cc.dopts.unaryInt 作为第一个元素
	if cc.dopts.unaryInt != nil {
		interceptors = append([]UnaryClientInterceptor{cc.dopts.unaryInt}, interceptors...)
	}

	var chainedInt UnaryClientInterceptor
	if len(interceptors) == 0 {
		// 不存在拦截器
		chainedInt = nil
	} else if len(interceptors) == 1 {
		//	一个拦截器
		chainedInt = interceptors[0]
	} else {
		//////////////////////////////////////
		// todo 追代码
		// 如果设置了多个拦截器，则生成一个调用链
		//////////////////////////////////////
		chainedInt = func(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, invoker UnaryInvoker, opts ...CallOption) error {
			return interceptors[0](ctx, method, req, reply, cc, getChainUnaryInvoker(interceptors, 0, invoker), opts...)
		}
	}

	cc.dopts.unaryInt = chainedInt
}

// getChainUnaryInvoker recursively generate the chained unary invoker.
// 递归的生成 拦截器 调用链
func getChainUnaryInvoker(interceptors []UnaryClientInterceptor, curr int, finalInvoker UnaryInvoker) UnaryInvoker {
	// 是最后一个拦截器
	if curr == len(interceptors)-1 {
		return finalInvoker
	}

	// 通过 getChainUnaryInvoker 方法进行递归
	// 递归的重点是 在  对curr+1传给下一次递归，当curr==拦截器长度，则就是最后一个
	return func(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error {
		return interceptors[curr+1](ctx, method, req, reply, cc, getChainUnaryInvoker(interceptors, curr+1, finalInvoker), opts...)
	}
}

// chainStreamClientInterceptors chains all stream client interceptors into one.
//
// 合并流式拦截器
func chainStreamClientInterceptors(cc *ClientConn) {
	interceptors := cc.dopts.chainStreamInts
	// Prepend dopts.streamInt to the chaining interceptors if it exists, since streamInt will
	// be executed before any other chained interceptors.
	if cc.dopts.streamInt != nil {
		interceptors = append([]StreamClientInterceptor{cc.dopts.streamInt}, interceptors...)
	}

	var chainedInt StreamClientInterceptor
	if len(interceptors) == 0 {
		chainedInt = nil
	} else if len(interceptors) == 1 {
		chainedInt = interceptors[0]
	} else {
		chainedInt = func(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, streamer Streamer, opts ...CallOption) (ClientStream, error) {
			return interceptors[0](ctx, desc, cc, method, getChainStreamer(interceptors, 0, streamer), opts...)
		}
	}

	cc.dopts.streamInt = chainedInt
}

// getChainStreamer recursively generate the chained client stream constructor.
func getChainStreamer(interceptors []StreamClientInterceptor, curr int, finalStreamer Streamer) Streamer {
	if curr == len(interceptors)-1 {
		return finalStreamer
	}
	return func(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (ClientStream, error) {
		return interceptors[curr+1](ctx, desc, cc, method, getChainStreamer(interceptors, curr+1, finalStreamer), opts...)
	}
}

// connectivityStateManager keeps the connectivity.State of ClientConn.
// This struct will eventually be exported so the balancers can access it.
//
// 保持 ClientConn 的 connectivity.State状态。
// 这个结构体最终将被导出，以便平衡器可以访问它。
type connectivityStateManager struct {
	mu sync.Mutex
	// 活跃的状态
	state connectivity.State

	// 通知的chan
	notifyChan chan struct{}
	channelzID int64
}

// updateState updates the connectivity.State of ClientConn.
// If there's a change it notifies goroutines waiting on state change to
// happen.
//
// 更新状态，如果有变化，它会通知goroutines等待状态变化的发生。
//
func (csm *connectivityStateManager) updateState(state connectivity.State) {
	csm.mu.Lock()
	defer csm.mu.Unlock()

	// 表示ClientConn已开始关闭
	if csm.state == connectivity.Shutdown {
		return
	}

	// 状态等于期待的 state 状态
	if csm.state == state {
		return
	}

	// 设置状态为 state
	csm.state = state
	channelz.Infof(logger, csm.channelzID, "Channel Connectivity change to %v", state)

	if csm.notifyChan != nil {
		// There are other goroutines waiting on this channel.
		close(csm.notifyChan)
		csm.notifyChan = nil
	}
}

func (csm *connectivityStateManager) getState() connectivity.State {
	csm.mu.Lock()
	defer csm.mu.Unlock()
	return csm.state
}

func (csm *connectivityStateManager) getNotifyChan() <-chan struct{} {
	csm.mu.Lock()
	defer csm.mu.Unlock()
	if csm.notifyChan == nil {
		csm.notifyChan = make(chan struct{})
	}
	return csm.notifyChan
}

// ClientConnInterface defines the functions clients need to perform unary and
// streaming RPCs.  It is implemented by *ClientConn, and is only intended to
// be referenced by generated code.
type ClientConnInterface interface {
	// Invoke performs a unary RPC and returns after the response is received
	// into reply.
	Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...CallOption) error
	// NewStream begins a streaming RPC.
	NewStream(ctx context.Context, desc *StreamDesc, method string, opts ...CallOption) (ClientStream, error)
}

// Assert *ClientConn implements ClientConnInterface.
var _ ClientConnInterface = (*ClientConn)(nil)

// ClientConn represents a virtual connection to a conceptual endpoint, to
// perform RPCs.
//
// A ClientConn is free to have zero or more actual connections to the endpoint
// based on configuration, load, etc. It is also free to determine which actual
// endpoints to use and may change it every RPC, permitting client-side load
// balancing.
//
// A ClientConn encapsulates a range of functionality including name
// resolution, TCP connection establishment (with retries and backoff) and TLS
// handshakes. It also handles errors on established connections by
// re-resolving the name and reconnecting.
type ClientConn struct {
	// 上下文
	ctx    context.Context
	cancel context.CancelFunc

	// 客户端目标地址
	target string

	parsedTarget resolver.Target
	authority    string

	// dialOptions 使用dial时的配置项
	dopts dialOptions

	// conn状态信息相关
	csMgr *connectivityStateManager

	// 创建 负载均衡器构造器 时使用的 options
	balancerBuildOpts balancer.BuildOptions
	blockingpicker    *pickerWrapper

	safeConfigSelector iresolver.SafeConfigSelector

	mu sync.RWMutex

	// 把 名称解析器 包装之后的对象
	resolverWrapper *ccResolverWrapper
	sc              *ServiceConfig

	// *addrConn 每一个地址创建的连接对象
	// addrConn 中 transport 字段保存着创建好的连接
	conns map[*addrConn]struct{}

	// Keepalive parameter can be updated if a GoAway is received.
	// 如果接收到超时，则更新Keepalive参数
	mkp keepalive.ClientParameters

	// 负载均衡器名称
	curBalancerName string
	// 负载均衡器包装
	balancerWrapper *ccBalancerWrapper

	// 重试相关信息
	retryThrottler atomic.Value

	// 第一次解析之后会触发的事件
	firstResolveEvent *grpcsync.Event

	// channelz 相关
	channelzID int64 // channelz unique identification number
	czData     *channelzData

	// conn的最后一个错误信息相关
	lceMu sync.Mutex // protects lastConnectionError

	// 记录最后连接一个error
	lastConnectionError error
}

// WaitForStateChange waits until the connectivity.State of ClientConn changes from sourceState or
// ctx expires. A true value is returned in former case and false in latter.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
//
// 等待状态发送变化，发生变化并且不等于sourceState，则返回true
//
func (cc *ClientConn) WaitForStateChange(ctx context.Context, sourceState connectivity.State) bool {
	// 获取chan
	// notifyChan 这个 channel 仅通过 close 做广播性的通知
	// 每当 state 状态变化会惰性产生新的 notifyChan
	// 当这个 notifyChan 被关闭时就意味着状态有变化了，起到一个类似条件变量的作用
	ch := cc.csMgr.getNotifyChan()

	// 获取状态 并判断是否等于 sourceState，则返回true
	if cc.csMgr.getState() != sourceState {
		return true
	}

	select {
	// 超时 返回false
	case <-ctx.Done():
		return false
	//	状态发生了变化 返回true
	case <-ch:
		return true
	}
}

// GetState returns the connectivity.State of ClientConn.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func (cc *ClientConn) GetState() connectivity.State {
	return cc.csMgr.getState()
}

// 监听 scChan 的变化
// 有数据变更，则更新
func (cc *ClientConn) scWatcher() {
	for {
		select {
		case sc, ok := <-cc.dopts.scChan:
			// 说明 通道 scChan 已经关闭了
			if !ok {
				return
			}

			cc.mu.Lock()
			// TODO: load balance policy runtime change is ignored.
			// We may revisit this decision in the future.
			// 我们将来可能会重新考虑这个决定
			cc.sc = &sc
			cc.safeConfigSelector.UpdateConfigSelector(&defaultConfigSelector{&sc})

			cc.mu.Unlock()
		case <-cc.ctx.Done():
			return
		}
	}
}

// waitForResolvedAddrs blocks until the resolver has provided addresses or the
// context expires.  Returns nil unless the context expires first; otherwise
// returns a status error based on the context.
//
// waitForResolvedAddrs 会阻塞，直到解析器提供了地址或者上下文过期
// 返回nil，除非上下文先过期;否则根据上下文返回一个状态错误
func (cc *ClientConn) waitForResolvedAddrs(ctx context.Context) error {
	// This is on the RPC path, so we use a fast path to avoid the
	// more-expensive "select" below after the resolver has returned once.
	//
	// func (cc *ClientConn) updateResolverState(s resolver.State, err error) error
	// defer cc.firstResolveEvent.Fire()
	if cc.firstResolveEvent.HasFired() {
		return nil
	}

	select {
	// 名称解析已经完毕invoke，会关闭 firstResolveEvent 中的 chan TODO 追代码逻辑
	case <-cc.firstResolveEvent.Done():
		return nil
	// 超时或者取消 这里的ctx是在调用请求方法时传入的ctx
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	// 超时或者取消 这里的ctx是在 Dial() 时传入的ctx
	case <-cc.ctx.Done():
		return ErrClientConnClosing
	}
}

var emptyServiceConfig *ServiceConfig

func init() {
	cfg := parseServiceConfig("{}")
	if cfg.Err != nil {
		panic(fmt.Sprintf("impossible error parsing empty service config: %v", cfg.Err))
	}
	emptyServiceConfig = cfg.Config.(*ServiceConfig)
}

// ！！！
// 根据配置信息 选择 创建 balancerWrapper 的方式
func (cc *ClientConn) maybeApplyDefaultServiceConfig(addrs []resolver.Address) {
	// sc  *ServiceConfig 不为空
	if cc.sc != nil {
		cc.applyServiceConfigAndBalancer(cc.sc, nil, addrs)
		return
	}

	if cc.dopts.defaultServiceConfig != nil {
		cc.applyServiceConfigAndBalancer(cc.dopts.defaultServiceConfig, &defaultConfigSelector{cc.dopts.defaultServiceConfig}, addrs)
	} else {
		// TODO (追源码)
		// 一般 没有配置sc 会走这里
		cc.applyServiceConfigAndBalancer(emptyServiceConfig, &defaultConfigSelector{emptyServiceConfig}, addrs)
	}
}

// TODO （read code）
//  更新 名称解析器 解析出来的地址列表到 负载均衡 中
//  s resolver.State  名称解析器解析出来的服务端地址列表
//
//  第二个参数 err ：因为会在不同的地方多次调用，所以传入其他地方的操作结果是否有错误发生
func (cc *ClientConn) updateResolverState(s resolver.State, err error) error {
	// 此标记信息 在 grpc.ClientConn 对象 firstResolveEvent 中
	// 标识 第一次名称解析已经就绪，主要是在 invoke 调用时，需要等待地址连接就绪

	// 重要！
	// 由于 此方法可能 不跟 Dial() 方法在同一个协程里面，所以需要 defer 延迟执行 Fire()
	// 这样，Dial() 方法所在协程能通过 firstResolveEvent 来检测到名称解析逻辑是否就绪
	defer cc.firstResolveEvent.Fire()

	// ！！！加锁
	cc.mu.Lock()
	// Check if the ClientConn is already closed. Some fields (e.g.
	// balancerWrapper) are set to nil when closing the ClientConn, and could
	// cause nil pointer panic if we don't have this check.
	if cc.conns == nil {
		cc.mu.Unlock()
		return nil
	}

	// 进入当前方法 带进来的err 不为 nil，说明有错误
	if err != nil {
		// May need to apply the initial service config in case the resolver
		// doesn't support service configs, or doesn't provide a service config
		// with the new addresses.
		//
		// 可能需要应用初始服务配置的情况下，解析器不支持服务配置
		// 或者不提供服务配置使用新的地址
		cc.maybeApplyDefaultServiceConfig(nil)

		if cc.balancerWrapper != nil {
			cc.balancerWrapper.resolverError(err)
		}

		// No addresses are valid with err set; return early.
		cc.mu.Unlock()
		return balancer.ErrBadResolverState
	}

	var ret error
	////////////////////////////////////////////////////////
	// TODO (追源码)
	//  如果不存在 负载均衡器的包装对象，则创建 负载均衡器的包装对象
	//  这里是获取 负载均衡器的包装对象 并赋值给 cc.balancerWrapper
	////////////////////////////////////////////////////////

	// ServiceConfig 为空，则试图使用默认的负载均衡器
	if cc.dopts.disableServiceConfig || s.ServiceConfig == nil {
		//////////////////////////////////////////////////////
		// 没有配置 service config
		// !!! 创建 默认的 grpc.ClientConn 的 balancerWrapper
		//////////////////////////////////////////////////////
		cc.maybeApplyDefaultServiceConfig(s.Addresses)
		// TODO: do we need to apply a failing LB policy if there is no
		// default, per the error handling design?
		// 默认，根据错误处理设计?
	} else {
		// 用户通过 ServiceConfig 配置了 负载均衡器信息
		if sc, ok := s.ServiceConfig.Config.(*ServiceConfig); s.ServiceConfig.Err == nil && ok {
			configSelector := iresolver.GetConfigSelector(s)

			if configSelector != nil {
				if len(s.ServiceConfig.Config.(*ServiceConfig).Methods) != 0 {
					channelz.Infof(logger, cc.channelzID, "method configs in service config will be ignored due to presence of config selector")
				}
			} else {
				configSelector = &defaultConfigSelector{sc}
			}

			////////////////////////////////////////////
			// ServiceConfig 配置了 Balancer
			// 此时 cc 中已经获取到了对应的负载均衡器对象
			////////////////////////////////////////////
			cc.applyServiceConfigAndBalancer(sc, configSelector, s.Addresses)
		} else {
			ret = balancer.ErrBadResolverState

			if cc.balancerWrapper == nil {
				var err error
				if s.ServiceConfig.Err != nil {
					err = status.Errorf(codes.Unavailable, "error parsing service config: %v", s.ServiceConfig.Err)
				} else {
					err = status.Errorf(codes.Unavailable, "illegal service config type: %T", s.ServiceConfig.Config)
				}

				cc.safeConfigSelector.UpdateConfigSelector(&defaultConfigSelector{cc.sc})

				// 更新负载均衡器的选择器
				cc.blockingpicker.updatePicker(base.NewErrPicker(err))
				cc.csMgr.updateState(connectivity.TransientFailure)
				cc.mu.Unlock()

				return ret
			}
		}
	}

	// 走到这里 能确定 cc.balancerWrapper 是存在的

	var balCfg serviceconfig.LoadBalancingConfig
	if cc.dopts.balancerBuilder == nil && cc.sc != nil && cc.sc.lbConfig != nil {
		balCfg = cc.sc.lbConfig.cfg
	}

	cbn := cc.curBalancerName // 负载均衡器名称
	bw := cc.balancerWrapper  // 负载均衡器的包装对象
	cc.mu.Unlock()

	// 当前负载均衡器的名字 不是 grpclb
	// ！！！过滤掉 grpclb 负载均衡器的地址
	if cbn != grpclbName {
		// Filter any grpclb addresses since we don't have the grpclb balancer.
		// 过滤掉 grpclb 负载均衡器的地址
		for i := 0; i < len(s.Addresses); {
			// 过滤掉  s.Addresses[i]
			// TODO ！！！ 切片的裁剪方法值得学习
			if s.Addresses[i].Type == resolver.GRPCLB {
				// 删减第 i 个元素
				copy(s.Addresses[i:], s.Addresses[i+1:])
				//裁剪 slice，长度-1
				s.Addresses = s.Addresses[:len(s.Addresses)-1]
				continue
			}

			i++
		}
	}

	////////////////////////////////////////////////////////////
	// TODO (追源码，学习)
	//  updateClientConnState 为 balancer_conn_wrappers.go 的方法
	//  bw 为负载均衡器包装对象
	//  负载均衡器 根据 服务端地址列表更新 conn 状态
	/////////////////////////////////////////////////////////////
	uccsErr := bw.updateClientConnState(&balancer.ClientConnState{ResolverState: s, BalancerConfig: balCfg})
	if ret == nil {
		ret = uccsErr // prefer ErrBadResolver state since any other error is
		// currently meaningless to the caller.
	}

	return ret
}

// switchBalancer starts the switching from current balancer to the balancer
// with the given name.
//
// It will NOT send the current address list to the new balancer. If needed,
// caller of this function should send address list to the new balancer after
// this function returns.
//
// Caller must hold cc.mu.
//
//  ！！！创建 ccBalancerWrapper
func (cc *ClientConn) switchBalancer(name string) {
	if strings.EqualFold(cc.curBalancerName, name) {
		return
	}

	channelz.Infof(logger, cc.channelzID, "ClientConn switching balancer to %q", name)
	if cc.dopts.balancerBuilder != nil {
		channelz.Info(logger, cc.channelzID, "ignoring balancer switching: Balancer DialOption used instead")
		return
	}
	if cc.balancerWrapper != nil {
		// Don't hold cc.mu while closing the balancers. The balancers may call
		// methods that require cc.mu (e.g. cc.NewSubConn()). Holding the mutex
		// would cause a deadlock in that case.
		cc.mu.Unlock()
		cc.balancerWrapper.close()
		cc.mu.Lock()
	}

	builder := balancer.Get(name)
	// 如果负载均衡器的构建器为空，则选择 pick_first
	if builder == nil {
		channelz.Warningf(logger, cc.channelzID, "Channel switches to new LB policy %q due to fallback from invalid balancer name", PickFirstBalancerName)
		channelz.Infof(logger, cc.channelzID, "failed to get balancer builder for: %v, using pick_first instead", name)
		builder = newPickfirstBuilder()
	} else {
		channelz.Infof(logger, cc.channelzID, "Channel switches to new LB policy %q", name)
	}

	// 如果是 roundrobin 则在 balancer/roundrobin/roundrobin.go文件中
	// roundrobin 又使用了 balancer/base/base.go
	//
	// Balancer 名称
	cc.curBalancerName = builder.Name()
	// Balancer 对象
	cc.balancerWrapper = newCCBalancerWrapper(cc, builder, cc.balancerBuildOpts)
}

func (cc *ClientConn) handleSubConnStateChange(sc balancer.SubConn, s connectivity.State, err error) {
	cc.mu.Lock()
	if cc.conns == nil {
		cc.mu.Unlock()
		return
	}

	// TODO(bar switching) send updates to all balancer wrappers when balancer
	// gracefully switching is supported.
	cc.balancerWrapper.handleSubConnStateChange(sc, s, err)

	cc.mu.Unlock()
}

// newAddrConn creates an addrConn for addrs and adds it to cc.conns.
//
// Caller needs to make sure len(addrs) > 0.
//
// 为每一个地址创建一个 addrConn 对象
func (cc *ClientConn) newAddrConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (*addrConn, error) {
	ac := &addrConn{
		state:        connectivity.Idle,
		cc:           cc,
		addrs:        addrs, // 地址数组
		scopts:       opts,
		dopts:        cc.dopts,
		czData:       new(channelzData),
		resetBackoff: make(chan struct{}),
	}

	// 创建可以取消的ctx
	ac.ctx, ac.cancel = context.WithCancel(cc.ctx)

	// Track ac in cc. This needs to be done before any getTransport(...) is called.
	cc.mu.Lock()
	if cc.conns == nil {
		cc.mu.Unlock()
		return nil, ErrClientConnClosing
	}

	if channelz.IsOn() {
		ac.channelzID = channelz.RegisterSubChannel(ac, cc.channelzID, "")
		channelz.AddTraceEvent(logger, ac.channelzID, 0, &channelz.TraceEventDesc{
			Desc:     "Subchannel Created",
			Severity: channelz.CtInfo,
			Parent: &channelz.TraceEventDesc{
				Desc:     fmt.Sprintf("Subchannel(id:%d) created", ac.channelzID),
				Severity: channelz.CtInfo,
			},
		})
	}

	// ！！！！
	// 把创建的 对象地址 存储在 grpc.ClientConn 的 conns map 中
	// 主要是为了快速判断 addrConn 对象是都存在
	cc.conns[ac] = struct{}{}

	cc.mu.Unlock()
	return ac, nil
}

// removeAddrConn removes the addrConn in the subConn from clientConn.
// It also tears down the ac with the given error.
func (cc *ClientConn) removeAddrConn(ac *addrConn, err error) {
	cc.mu.Lock()
	if cc.conns == nil {
		cc.mu.Unlock()
		return
	}

	// 删除 cc.conns 字段 map 中的 ac
	delete(cc.conns, ac)

	cc.mu.Unlock()

	// 销毁 ac
	ac.tearDown(err)
}

func (cc *ClientConn) channelzMetric() *channelz.ChannelInternalMetric {
	return &channelz.ChannelInternalMetric{
		State:                    cc.GetState(),
		Target:                   cc.target,
		CallsStarted:             atomic.LoadInt64(&cc.czData.callsStarted),
		CallsSucceeded:           atomic.LoadInt64(&cc.czData.callsSucceeded),
		CallsFailed:              atomic.LoadInt64(&cc.czData.callsFailed),
		LastCallStartedTimestamp: time.Unix(0, atomic.LoadInt64(&cc.czData.lastCallStartedTime)),
	}
}

// Target returns the target string of the ClientConn.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func (cc *ClientConn) Target() string {
	return cc.target
}

func (cc *ClientConn) incrCallsStarted() {
	atomic.AddInt64(&cc.czData.callsStarted, 1)
	atomic.StoreInt64(&cc.czData.lastCallStartedTime, time.Now().UnixNano())
}

func (cc *ClientConn) incrCallsSucceeded() {
	atomic.AddInt64(&cc.czData.callsSucceeded, 1)
}

func (cc *ClientConn) incrCallsFailed() {
	atomic.AddInt64(&cc.czData.callsFailed, 1)
}

// connect starts creating a transport.
// It does nothing if the ac is not IDLE.
// TODO(bar) Move this to the addrConn section.
//
// ！！！！ 重要 ！！！！
// 对 addrConn 建立连接
func (ac *addrConn) connect() error {
	ac.mu.Lock()
	if ac.state == connectivity.Shutdown {
		ac.mu.Unlock()
		return errConnClosing
	}

	if ac.state != connectivity.Idle {
		ac.mu.Unlock()
		return nil
	}

	// Update connectivity state within the lock to prevent subsequent or
	// concurrent calls from resetting the transport more than once.

	// TODO
	//  更新连接状态为 连接中 connectivity.Connecting
	ac.updateConnectivityState(connectivity.Connecting, nil)
	ac.mu.Unlock()

	// 打印整个调用链
	//debug.PrintStack()

	//////////////////////////////////////////////////////////////
	// Start a goroutine connecting to the server asynchronously.
	// ！！！核心
	// 启动一个异步连接到服务器的goroutine。
	// 所以说 连接建立过程是异步的哦
	/////////////////////////////////////////////////////////////

	// ac.resetTransport() 方法是个死循环，一般不会退出
	// 所以启动新的协程
	// 在 resetTransport 中会对 ac 中的地址创建一个连接
	// 也就是说 ac 一直对应一个可用的地址连接
	go ac.resetTransport()

	return nil
}

// tryUpdateAddrs tries to update ac.addrs with the new addresses list.
// 试图使用 新的 地址列表更新 ac.addrs
//
// If ac is Connecting, it returns false. The caller should tear down the ac and
// create a new one. Note that the backoff will be reset when this happens.
// 如果 ac 是连接状态，则返回 false
// 调用者 caller应该销毁 ac，并创建一个新的 ac
//
// If ac is TransientFailure, it updates ac.addrs and returns true. The updated
// addresses will be picked up by retry in the next iteration after backoff.
//
// If ac is Shutdown or Idle, it updates ac.addrs and returns true.
//
// If ac is Ready, it checks whether current connected address of ac is in the
// new addrs list.
// 如果 ac 是就绪状态，则检查 当前连接地址是否在 新的 addr 地址列表中
//
//  - If true, it updates ac.addrs and returns true. The ac will keep using
//    the existing connection.
//  - If false, it does nothing and returns false.
func (ac *addrConn) tryUpdateAddrs(addrs []resolver.Address) bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	channelz.Infof(logger, ac.channelzID, "addrConn: tryUpdateAddrs curAddr: %v, addrs: %v", ac.curAddr, addrs)

	if ac.state == connectivity.Shutdown ||
		ac.state == connectivity.TransientFailure ||
		ac.state == connectivity.Idle {
		ac.addrs = addrs
		return true
	}

	// 如果 ac 是连接中，则直接返回
	if ac.state == connectivity.Connecting {
		return false
	}

	// ac.state is Ready, try to find the connected address.
	// ac 连接状态是 就绪，则从 address 找出建立连接时用的 那个地址
	var curAddrFound bool
	for _, a := range addrs {
		// ac.curAddr 是 addrconn 当前建立连接 对应的一个地址
		// 找到对应地址
		if reflect.DeepEqual(ac.curAddr, a) {
			curAddrFound = true
			break
		}
	}

	channelz.Infof(logger, ac.channelzID, "addrConn: tryUpdateAddrs curAddrFound: %v", curAddrFound)

	// 找到，则更新对应的 ac.addrs
	// ac.addrs 是一个地址切片，其中包含已经建立连接的地址
	if curAddrFound {
		ac.addrs = addrs
	}

	// 返回 是否 找到建立连接地址
	return curAddrFound
}

func getMethodConfig(sc *ServiceConfig, method string) MethodConfig {
	if sc == nil {
		return MethodConfig{}
	}
	if m, ok := sc.Methods[method]; ok {
		return m
	}
	i := strings.LastIndex(method, "/")
	if m, ok := sc.Methods[method[:i+1]]; ok {
		return m
	}
	return sc.Methods[""]
}

// GetMethodConfig gets the method config of the input method.
// If there's an exact match for input method (i.e. /service/method), we return
// the corresponding MethodConfig.
// If there isn't an exact match for the input method, we look for the service's default
// config under the service (i.e /service/) and then for the default for all services (empty string).
//
// If there is a default MethodConfig for the service, we return it.
// Otherwise, we return an empty MethodConfig.
func (cc *ClientConn) GetMethodConfig(method string) MethodConfig {
	// TODO: Avoid the locking here.
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return getMethodConfig(cc.sc, method)
}

func (cc *ClientConn) healthCheckConfig() *healthCheckConfig {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.sc == nil {
		return nil
	}
	return cc.sc.healthCheckConfig
}

// 负载均衡器 选择一个可用的连接
func (cc *ClientConn) getTransport(ctx context.Context, failfast bool, method string) (transport.ClientTransport, func(balancer.DoneInfo), error) {
	//////////////////////////////
	// TODO (追源码)
	// 	负载均衡选择器 选择一个连接
	//////////////////////////////
	t, done, err := cc.blockingpicker.pick(ctx, failfast, balancer.PickInfo{
		Ctx:            ctx,
		FullMethodName: method,
	})

	if err != nil {
		return nil, nil, toRPCErr(err)
	}

	return t, done, nil
}

// TODO （read code）
//  创建 balancerWrapper
func (cc *ClientConn) applyServiceConfigAndBalancer(sc *ServiceConfig, configSelector iresolver.ConfigSelector, addrs []resolver.Address) {
	if sc == nil {
		// should never reach here.
		return
	}

	cc.sc = sc
	if configSelector != nil {
		cc.safeConfigSelector.UpdateConfigSelector(configSelector)
	}

	if cc.sc.retryThrottling != nil {
		newThrottler := &retryThrottler{
			tokens: cc.sc.retryThrottling.MaxTokens,
			max:    cc.sc.retryThrottling.MaxTokens,
			thresh: cc.sc.retryThrottling.MaxTokens / 2,
			ratio:  cc.sc.retryThrottling.TokenRatio,
		}
		cc.retryThrottler.Store(newThrottler)
	} else {
		cc.retryThrottler.Store((*retryThrottler)(nil))
	}

	////////////////////////////////////////////
	// 创建 balancerWrapper
	////////////////////////////////////////////

	// 下面2中情况需要进行 负载均衡器的构造器初始化
	// 1. cc.dopts.balancerBuilder 不存在
	//	 grpc.WithBalancerName(roundrobin.Name 设置的 balancerBuilder
	// 2. cc.balancerWrapper  不存在
	//
	// 如果 存在，则不会再走下面逻辑

	// 负载均衡器的构造器 为空
	// 通过 grpc.WithBalancerName("round_robin") 设置 cc.dopts.balancerBuilder
	if cc.dopts.balancerBuilder == nil {
		// Only look at balancer types and switch balancer if balancer dial
		// option is not set.
		// 如果没有设置平衡器拨号选项，只查看平衡器类型和开关平衡器
		//
		// 负载均衡器名成
		var newBalancerName string

		// 从 config 中获取 负载均衡器，名称
		if cc.sc != nil && cc.sc.lbConfig != nil {
			newBalancerName = cc.sc.lbConfig.name
		} else {
			// config 没有 负载均衡器名称
			var isGRPCLB bool

			// 判断地址类型是否是 resolver.GRPCLB
			for _, a := range addrs {
				if a.Type == resolver.GRPCLB {
					isGRPCLB = true
					break
				}
			}

			// 是 GRPCLB，则负载均衡器名称为 grpclbName
			if isGRPCLB {
				newBalancerName = grpclbName
			} else if cc.sc != nil && cc.sc.LB != nil {
				// 配置信息存在 负载均衡器名称
				newBalancerName = *cc.sc.LB
			} else {
				////////////////////////////////
				// ！！！
				// 默认使用 pick_first Balancer
				////////////////////////////////
				newBalancerName = PickFirstBalancerName
			}
		}

		// 听过负载均衡器名称 获取 负载均衡器的构造器
		// 并对 cc.balancerWrapper  cc.curBalancerName 进行赋值
		cc.switchBalancer(newBalancerName)
	} else if cc.balancerWrapper == nil {
		// Balancer dial option was set, and this is the first time handling
		// resolved addresses. Build a balancer with dopts.balancerBuilder.

		// 使用 grpc.WithBalancerName(roundrobin.Name) 的话，会进到这里，对 Builder进行包装
		// 创建一个负载均衡器包装器对象
		cc.curBalancerName = cc.dopts.balancerBuilder.Name()
		cc.balancerWrapper = newCCBalancerWrapper(cc, cc.dopts.balancerBuilder, cc.balancerBuildOpts)
	}
}

// ResolveNow将被gRPC调用以尝试再次解析目标名称
// 这只是一个提示，如果没有必要，解析器可以忽略它。它可以同时被多次调用
func (cc *ClientConn) resolveNow(o resolver.ResolveNowOptions) {
	cc.mu.RLock()
	r := cc.resolverWrapper
	cc.mu.RUnlock()
	if r == nil {
		return
	}

	// 使用新的 goroutine 再次 解析 目标名称
	// ResolveNow将被gRPC调用以尝试再次解析目标名称
	// 这只是一个提示，如果没有必要，解析器可以忽略它。它可以同时被多次调用
	//
	// r 为名称解析器
	// 使用 新的 goroutine 是因为，r.resolveNo 可能会出现阻塞
	go r.resolveNow(o)
}

// ResetConnectBackoff wakes up all subchannels in transient failure and causes
// them to attempt another connection immediately.  It also resets the backoff
// times used for subsequent attempts regardless of the current state.
//
// In general, this function should not be used.  Typical service or network
// outages result in a reasonable client reconnection strategy by default.
// However, if a previously unavailable network becomes available, this may be
// used to trigger an immediate reconnect.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func (cc *ClientConn) ResetConnectBackoff() {
	cc.mu.Lock()
	conns := cc.conns
	cc.mu.Unlock()
	for ac := range conns {
		ac.resetConnectBackoff()
	}
}

// Close tears down the ClientConn and all underlying connections.
//
// 销毁每个 客户端连接 ClientConn
func (cc *ClientConn) Close() error {
	defer cc.cancel()

	cc.mu.Lock()
	if cc.conns == nil {
		cc.mu.Unlock()
		return ErrClientConnClosing
	}
	conns := cc.conns
	cc.conns = nil
	cc.csMgr.updateState(connectivity.Shutdown)

	rWrapper := cc.resolverWrapper
	cc.resolverWrapper = nil
	bWrapper := cc.balancerWrapper
	cc.balancerWrapper = nil
	cc.mu.Unlock()

	cc.blockingpicker.close()

	// 关闭 负载均衡、名称解析 包装器
	if bWrapper != nil {
		bWrapper.close()
	}
	if rWrapper != nil {
		rWrapper.close()
	}

	// 销毁 每一个连接
	for ac := range conns {
		ac.tearDown(ErrClientConnClosing)
	}

	if channelz.IsOn() {
		ted := &channelz.TraceEventDesc{
			Desc:     "Channel Deleted",
			Severity: channelz.CtInfo,
		}
		if cc.dopts.channelzParentID != 0 {
			ted.Parent = &channelz.TraceEventDesc{
				Desc:     fmt.Sprintf("Nested channel(id:%d) deleted", cc.channelzID),
				Severity: channelz.CtInfo,
			}
		}
		channelz.AddTraceEvent(logger, cc.channelzID, 0, ted)
		// TraceEvent needs to be called before RemoveEntry, as TraceEvent may add trace reference to
		// the entity being deleted, and thus prevent it from being deleted right away.
		channelz.RemoveEntry(cc.channelzID)
	}

	return nil
}

// addrConn is a network connection to a given address.
type addrConn struct {
	ctx    context.Context
	cancel context.CancelFunc

	cc     *ClientConn
	dopts  dialOptions
	acbw   balancer.SubConn
	scopts balancer.NewSubConnOptions

	// transport is set when there's a viable transport (note: ac state may not be READY as LB channel
	// health checking may require server to report healthy to set ac to READY), and is reset
	// to nil when the current transport should no longer be used to create a stream (e.g. after GoAway
	// is received, transport is closed, ac has been torn down).
	transport transport.ClientTransport // The current transport.

	mu      sync.Mutex
	curAddr resolver.Address   // The current address.
	addrs   []resolver.Address // All addresses that the resolver resolved to.

	// Use updateConnectivityState for updating addrConn's connectivity state.
	state connectivity.State

	backoffIdx   int // Needs to be stateful for resetConnectBackoff.
	resetBackoff chan struct{}

	channelzID int64 // channelz unique identification number.
	czData     *channelzData
}

// Note: this requires a lock on ac.mu.
func (ac *addrConn) updateConnectivityState(s connectivity.State, lastErr error) {
	if ac.state == s {
		return
	}

	ac.state = s
	channelz.Infof(logger, ac.channelzID, "Subchannel Connectivity change to %v", s)
	ac.cc.handleSubConnStateChange(ac.acbw, s, lastErr)
}

// adjustParams updates parameters used to create transports upon
// receiving a GoAway.
func (ac *addrConn) adjustParams(r transport.GoAwayReason) {
	switch r {
	case transport.GoAwayTooManyPings:
		v := 2 * ac.dopts.copts.KeepaliveParams.Time
		ac.cc.mu.Lock()
		if v > ac.cc.mkp.Time {
			ac.cc.mkp.Time = v
		}
		ac.cc.mu.Unlock()
	}
}

// TODO (read code)
//  对 ac.addrs 创建连接
//  启动新的协程执行该方法
func (ac *addrConn) resetTransport() {
	// 循环
	for i := 0; ; i++ {
		// ！！！！
		// 第一次尝试建立连接失败后，后面的每次都要去再次解析目标地址
		if i > 0 {
			// 走到这个逻辑，说明 在这个循环中 已经不是第一次尝试建立连接，面前的都失败了
			// 所以尝试再次解释 目标名称
			//
			// ResolveNow将被gRPC调用以尝试再次解析目标名称
			// 这只是一个提示，如果没有必要，解析器可以忽略它。它可以同时被多次调用
			//
			// 很可能在 名称解析器中 ResolveNow 方法时空方法

			// ac.cc 为 grpc.ClientConn

			// 再次解析，获取新的地址列表
			ac.cc.resolveNow(resolver.ResolveNowOptions{})
		}

		ac.mu.Lock()
		// 状态显示已经关闭，则退出
		if ac.state == connectivity.Shutdown {
			ac.mu.Unlock()
			return
		}

		// 所有解析出来的服务端地址列表
		addrs := ac.addrs

		backoffFor := ac.dopts.bs.Backoff(ac.backoffIdx)
		// This will be the duration that dial gets to finish.
		// 最小的连接时间
		dialDuration := minConnectTimeout
		if ac.dopts.minConnectTimeout != nil {
			dialDuration = ac.dopts.minConnectTimeout()
		}

		if dialDuration < backoffFor {
			// Give dial more time as we keep failing to connect.
			dialDuration = backoffFor
		}
		// We can potentially spend all the time trying the first address, and
		// if the server accepts the connection and then hangs, the following
		// addresses will never be tried.
		//
		// The spec doesn't mention what should be done for multiple addresses.
		// https://github.com/grpc/grpc/blob/master/doc/connection-backoff.md#proposed-backoff-algorithm
		connectDeadline := time.Now().Add(dialDuration)

		// 更新ac的状态 为 连接中
		ac.updateConnectivityState(connectivity.Connecting, nil)
		ac.transport = nil // 置空 transport 字段，用来存储即将成功的连接
		ac.mu.Unlock()

		////////////////////////////////////////////////////
		// ！！！重要
		// 尝试在所有的地址取建立连接，直到遇到第一个连接建立成功
		////////////////////////////////////////////////////

		// addrs 可能是一个地址 也可能是多个
		// try 在所有的地址上建立连接，取第一个成功的
		newTr, addr, reconnect, err := ac.tryAllAddrs(addrs, connectDeadline)
		if err != nil {
			// After exhausting all addresses, the addrConn enters
			// TRANSIENT_FAILURE.
			ac.mu.Lock()
			if ac.state == connectivity.Shutdown {
				ac.mu.Unlock()
				return
			}

			// TODO (read code)
			//  连接建立失败，更新状态
			ac.updateConnectivityState(connectivity.TransientFailure, err)

			// Backoff.
			b := ac.resetBackoff
			ac.mu.Unlock()

			timer := time.NewTimer(backoffFor)
			select {
			case <-timer.C: // 设置定时器，等待下一次再开始重试
				ac.mu.Lock()
				ac.backoffIdx++ // 重试次数+1
				ac.mu.Unlock()
			case <-b:
				timer.Stop()
			case <-ac.ctx.Done(): // 超时 或 取消
				timer.Stop()
				return
			}

			// 失败，则重试
			continue
		}

		// 走到这里 说明
		// 创建连接成功
		ac.mu.Lock()
		if ac.state == connectivity.Shutdown {
			ac.mu.Unlock()
			newTr.Close(fmt.Errorf("reached connectivity state: SHUTDOWN"))
			return
		}

		// ！！！！
		ac.curAddr = addr    // curAddr 只存储 当前建立连接成功的一个地址
		ac.transport = newTr // 这里保存着创建好的 连接 newTr
		ac.backoffIdx = 0

		hctx, hcancel := context.WithCancel(ac.ctx)

		// TODO (read code)
		//  ！！！启动新的 goroutine 进行健康检查
		//  很重要，会在该方法中 更新状态为 就绪
		//  ac.updateConnectivityState(connectivity.Ready, nil)
		ac.startHealthCheck(hctx)
		ac.mu.Unlock()

		// Block until the created transport is down. And when this happens,
		// we restart from the top of the addr list.

		// ！！！！
		// 阻塞，直到创建的传输停止。当这种情况发生时，我们从addr列表的顶部重新启动

		// 上面连接建立成功，则会一直阻塞在这里
		// 直到创建的传输停止 (停止、关闭 会触发 close 在 reconnect.Done() 的 chan)
		// 并且此时 ac.state == connectivity.Shutdown，则此时会退出该循环

		// 否则会再次进入循环逻辑，非异常不会退出此方法
		// func (e *Event) Done() <-chan struct{}
		<-reconnect.Done()

		// 由于创建的传输停止，所以停止此次连接的心跳检查
		hcancel()
		// restart connecting - the top of the loop will set state to
		// CONNECTING.  This is against the current connectivity semantics doc,
		// however it allows for graceful behavior for RPCs not yet dispatched
		// - unfortunate timing would otherwise lead to the RPC failing even
		// though the TRANSIENT_FAILURE state (called for by the doc) would be
		// instantaneous.
		//
		// Ideally we should transition to Idle here and block until there is
		// RPC activity that leads to the balancer requesting a reconnect of
		// the associated SubConn.
	}
}

// tryAllAddrs tries to creates a connection to the addresses, and stop when at the
// first successful one. It returns the transport, the address and a Event in
// the successful case. The Event fires when the returned transport disconnects.
//
// 尝试创建到这些地址的连接，并在第一个成功的连接时停止。
// 在成功的情况下，它返回传输、地址和一个Event。当返回的传输断开连接时，事件将触发。
//
// addrs []resolver.Address 包含所有的解析出来的地址
func (ac *addrConn) tryAllAddrs(addrs []resolver.Address, connectDeadline time.Time) (transport.ClientTransport, resolver.Address, *grpcsync.Event, error) {
	var firstConnErr error
	for _, addr := range addrs {
		ac.mu.Lock()
		if ac.state == connectivity.Shutdown {
			ac.mu.Unlock()
			return nil, resolver.Address{}, nil, errConnClosing
		}

		ac.cc.mu.RLock()
		ac.dopts.copts.KeepaliveParams = ac.cc.mkp
		ac.cc.mu.RUnlock()

		// copts 包含 Dialer
		copts := ac.dopts.copts
		if ac.scopts.CredsBundle != nil {
			copts.CredsBundle = ac.scopts.CredsBundle
		}
		ac.mu.Unlock()

		channelz.Infof(logger, ac.channelzID, "Subchannel picks a new address %q to connect", addr.Addr)

		///////////////////////////////////////////////
		// TODO (追代码)
		//  在 addr 地址上进行连接
		///////////////////////////////////////////////
		newTr, reconnect, err := ac.createTransport(addr, copts, connectDeadline)
		if err == nil {
			return newTr, addr, reconnect, nil
		}

		if firstConnErr == nil {
			firstConnErr = err
		}

		ac.cc.updateConnectionError(err)
	}

	// Couldn't connect to any address.
	return nil, resolver.Address{}, nil, firstConnErr
}

// createTransport creates a connection to addr. It returns the transport and a
// Event in the successful case. The Event fires when the returned transport
// disconnects.
//
// 创建到addr的连接。它在成功的情况下返回传输和一个Event。当返回的传输断开连接时，事件将触发。
//
func (ac *addrConn) createTransport(addr resolver.Address, copts transport.ConnectOptions, connectDeadline time.Time) (transport.ClientTransport, *grpcsync.Event, error) {
	prefaceReceived := make(chan struct{})
	onCloseCalled := make(chan struct{})

	// 用来进行通知的事件
	reconnect := grpcsync.NewEvent()

	// addr.ServerName takes precedent over ClientConn authority, if present.
	if addr.ServerName == "" {
		addr.ServerName = ac.cc.authority
	}

	// 注意：
	// 同一个 once 只能执行2个不同方法中的一个
	once := sync.Once{}
	onGoAway := func(r transport.GoAwayReason) {
		ac.mu.Lock()
		ac.adjustParams(r)
		once.Do(func() {
			if ac.state == connectivity.Ready {
				// Prevent this SubConn from being used for new RPCs by setting its
				// state to Connecting.
				//
				// TODO: this should be Idle when grpc-go properly supports it.
				ac.updateConnectivityState(connectivity.Connecting, nil)
			}
		})
		ac.mu.Unlock()

		// ！！！重要！！！
		// 当前连接退出时，会触发这里，通知使用该连接的位置
		reconnect.Fire()
	}

	onClose := func() {
		ac.mu.Lock()
		once.Do(func() {
			if ac.state == connectivity.Ready {
				// Prevent this SubConn from being used for new RPCs by setting its
				// state to Connecting.
				//
				// 通过将它的状态设置为连接，防止这个SubConn被用于新的rpc。
				//
				// TODO: this should be Idle when grpc-go properly supports it.
				ac.updateConnectivityState(connectivity.Connecting, nil)
			}
		})
		ac.mu.Unlock()
		close(onCloseCalled)

		// ！！！重要！！！
		// 当前连接关闭时，会触发这里，通知使用该连接的位置
		reconnect.Fire()
	}

	// 序言被接收到时执行，用来通知，表示连接建立成功
	onPrefaceReceipt := func() {
		close(prefaceReceived)
	}

	// 连接设置超时时间
	connectCtx, cancel := context.WithDeadline(ac.ctx, connectDeadline)
	defer cancel()
	if channelz.IsOn() {
		copts.ChannelzParentID = ac.channelzID
	}

	///////////////////////////////////////////////
	// ！！！重要
	// 对 addr 地址 建立连接
	// copts
	///////////////////////////////////////////////
	newTr, err := transport.NewClientTransport(connectCtx, ac.cc.ctx, addr, copts, onPrefaceReceipt, onGoAway, onClose)
	if err != nil {
		// newTr is either nil, or closed.
		channelz.Warningf(logger, ac.channelzID, "grpc: addrConn.createTransport failed to connect to %v. Err: %v. Reconnecting...", addr, err)
		return nil, nil, err
	}

	// 上面连接虽然创建好了，但是要在这里校验一下 超时、取消等情况
	select {
	case <-time.After(time.Until(connectDeadline)): // 超时
		// We didn't get the preface in time.
		// 我们还没得到响应序言，连接就超时了
		newTr.Close(fmt.Errorf("failed to receive server preface within timeout"))
		channelz.Warningf(logger, ac.channelzID, "grpc: addrConn.createTransport failed to connect to %v: didn't receive server preface in time. Reconnecting...", addr)
		return nil, nil, errors.New("timed out waiting for server handshake")
	case <-prefaceReceived:
		// We got the preface - huzzah! things are good.
		// 我们得到了前言 ——万岁! 一起正常
		// 连接成功
	case <-onCloseCalled:
		// The transport has already closed - noop.
		// transport 已经停止，连接关闭
		return nil, nil, errors.New("connection closed")
		// TODO(deklerk) this should bail on ac.ctx.Done(). Add a test and fix.
	}

	return newTr, reconnect, nil
}

// startHealthCheck starts the health checking stream (RPC) to watch the health
// stats of this connection if health checking is requested and configured.
//
// LB channel health checking is enabled when all requirements below are met:
// 1. it is not disabled by the user with the WithDisableHealthCheck DialOption
// 2. internal.HealthCheckFunc is set by importing the grpc/health package
// 3. a service config with non-empty healthCheckConfig field is provided
// 4. the load balancer requests it
//
// It sets addrConn to READY if the health checking stream is not started.
//
// Caller must hold ac.mu.
func (ac *addrConn) startHealthCheck(ctx context.Context) {
	var healthcheckManagingState bool
	defer func() {
		if !healthcheckManagingState {
			// todo （read code）
			// 连接 就绪！！！
			ac.updateConnectivityState(connectivity.Ready, nil)
		}
	}()

	if ac.cc.dopts.disableHealthCheck {
		return
	}
	healthCheckConfig := ac.cc.healthCheckConfig()
	if healthCheckConfig == nil {
		return
	}
	if !ac.scopts.HealthCheckEnabled {
		return
	}
	healthCheckFunc := ac.cc.dopts.healthCheckFunc
	if healthCheckFunc == nil {
		// The health package is not imported to set health check function.
		//
		// TODO: add a link to the health check doc in the error message.
		channelz.Error(logger, ac.channelzID, "Health check is requested but health check function is not set.")
		return
	}

	healthcheckManagingState = true

	// Set up the health check helper functions.
	currentTr := ac.transport
	newStream := func(method string) (interface{}, error) {
		ac.mu.Lock()
		if ac.transport != currentTr {
			ac.mu.Unlock()
			return nil, status.Error(codes.Canceled, "the provided transport is no longer valid to use")
		}
		ac.mu.Unlock()
		return newNonRetryClientStream(ctx, &StreamDesc{ServerStreams: true}, method, currentTr, ac)
	}

	setConnectivityState := func(s connectivity.State, lastErr error) {
		ac.mu.Lock()
		defer ac.mu.Unlock()
		if ac.transport != currentTr {
			return
		}
		ac.updateConnectivityState(s, lastErr)
	}

	// Start the health checking stream.
	// 开始健康检查
	go func() {
		err := ac.cc.dopts.healthCheckFunc(ctx, newStream, setConnectivityState, healthCheckConfig.ServiceName)
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				channelz.Error(logger, ac.channelzID, "Subchannel health check is unimplemented at server side, thus health check is disabled")
			} else {
				channelz.Errorf(logger, ac.channelzID, "HealthCheckFunc exits with unexpected error %v", err)
			}
		}
	}()
}

func (ac *addrConn) resetConnectBackoff() {
	ac.mu.Lock()
	close(ac.resetBackoff)
	ac.backoffIdx = 0
	ac.resetBackoff = make(chan struct{})
	ac.mu.Unlock()
}

// getReadyTransport returns the transport if ac's state is READY or nil if not.
// 如果 ac 的 state 是 就绪状态，则 返回对应连接 transport
func (ac *addrConn) getReadyTransport() transport.ClientTransport {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.state == connectivity.Ready {
		return ac.transport
	}

	return nil
}

// tearDown starts to tear down the addrConn.
// 销毁 addrconn 连接
//
// Note that tearDown doesn't remove ac from ac.cc.conns, so the addrConn struct
// will leak. In most cases, call cc.removeAddrConn() instead.
// tearDown 不会 从 ac.cc.conns 中移除 ac，那样的话会造成泄漏。
// 在大多数情况下，直接调用 cc.removeAddrConn() 代替直接使用 tearDown
// 因为  cc.removeAddrConn() 中会调用 tearDown
func (ac *addrConn) tearDown(err error) {
	ac.mu.Lock()
	if ac.state == connectivity.Shutdown {
		ac.mu.Unlock()
		return
	}

	curTr := ac.transport
	ac.transport = nil

	// We have to set the state to Shutdown before anything else to prevent races
	// between setting the state and logic that waits on context cancellation / etc.

	// 设置 ac 的状态为关闭
	ac.updateConnectivityState(connectivity.Shutdown, nil)
	ac.cancel()
	ac.curAddr = resolver.Address{}

	// 关闭 底层建立的 http2 连接
	if err == errConnDrain && curTr != nil {
		// GracefulClose(...) may be executed multiple times when
		// i) receiving multiple GoAway frames from the server; or
		// ii) there are concurrent name resolver/Balancer triggered
		// address removal and GoAway.
		// We have to unlock and re-lock here because GracefulClose => Close => onClose, which requires locking ac.mu.
		ac.mu.Unlock()
		curTr.GracefulClose()
		ac.mu.Lock()
	}

	if channelz.IsOn() {
		channelz.AddTraceEvent(logger, ac.channelzID, 0, &channelz.TraceEventDesc{
			Desc:     "Subchannel Deleted",
			Severity: channelz.CtInfo,
			Parent: &channelz.TraceEventDesc{
				Desc:     fmt.Sprintf("Subchanel(id:%d) deleted", ac.channelzID),
				Severity: channelz.CtInfo,
			},
		})
		// TraceEvent needs to be called before RemoveEntry, as TraceEvent may add trace reference to
		// the entity being deleted, and thus prevent it from being deleted right away.
		channelz.RemoveEntry(ac.channelzID)
	}

	ac.mu.Unlock()
}

func (ac *addrConn) getState() connectivity.State {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.state
}

func (ac *addrConn) ChannelzMetric() *channelz.ChannelInternalMetric {
	ac.mu.Lock()
	addr := ac.curAddr.Addr
	ac.mu.Unlock()
	return &channelz.ChannelInternalMetric{
		State:                    ac.getState(),
		Target:                   addr,
		CallsStarted:             atomic.LoadInt64(&ac.czData.callsStarted),
		CallsSucceeded:           atomic.LoadInt64(&ac.czData.callsSucceeded),
		CallsFailed:              atomic.LoadInt64(&ac.czData.callsFailed),
		LastCallStartedTimestamp: time.Unix(0, atomic.LoadInt64(&ac.czData.lastCallStartedTime)),
	}
}

func (ac *addrConn) incrCallsStarted() {
	atomic.AddInt64(&ac.czData.callsStarted, 1)
	atomic.StoreInt64(&ac.czData.lastCallStartedTime, time.Now().UnixNano())
}

func (ac *addrConn) incrCallsSucceeded() {
	atomic.AddInt64(&ac.czData.callsSucceeded, 1)
}

func (ac *addrConn) incrCallsFailed() {
	atomic.AddInt64(&ac.czData.callsFailed, 1)
}

// 重试 相关信息
type retryThrottler struct {
	max    float64
	thresh float64
	ratio  float64

	mu     sync.Mutex
	tokens float64 // TODO(dfawley): replace with atomic and remove lock.
}

// throttle subtracts a retry token from the pool and returns whether a retry
// should be throttled (disallowed) based upon the retry throttling policy in
// the service config.
func (rt *retryThrottler) throttle() bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.tokens--
	if rt.tokens < 0 {
		rt.tokens = 0
	}
	return rt.tokens <= rt.thresh
}

func (rt *retryThrottler) successfulRPC() {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.tokens += rt.ratio
	if rt.tokens > rt.max {
		rt.tokens = rt.max
	}
}

type channelzChannel struct {
	cc *ClientConn
}

func (c *channelzChannel) ChannelzMetric() *channelz.ChannelInternalMetric {
	return c.cc.channelzMetric()
}

// ErrClientConnTimeout indicates that the ClientConn cannot establish the
// underlying connections within the specified timeout.
//
// Deprecated: This error is never returned by grpc and should not be
// referenced by users.
var ErrClientConnTimeout = errors.New("grpc: timed out when dialing")

// 找 名称解析器构造器 有优先级：
//
// 1.先从 dialOptions 中 grpc.WithResolvers(passthrough.NewBuilder()) 注册的找
// 2.再从 resolver.Register(passthrough.NewBuilder()) 注册的构造器 中找
func (cc *ClientConn) getResolver(scheme string) resolver.Builder {
	// 先从 dialOptions 中找名称解析器的构造器
	// 也就是在 Dial() 中 grpc.WithResolvers(passthrough.NewBuilder()),
	for _, rb := range cc.dopts.resolvers {
		if scheme == rb.Scheme() {
			return rb
		}
	}

	// 在 dialOptions 中没找到
	// 试图去找 通过 resolver.Register(passthrough.NewBuilder()) 注册的构造器
	return resolver.Get(scheme)
}

func (cc *ClientConn) updateConnectionError(err error) {
	cc.lceMu.Lock()
	cc.lastConnectionError = err
	cc.lceMu.Unlock()
}

func (cc *ClientConn) connectionError() error {
	cc.lceMu.Lock()
	defer cc.lceMu.Unlock()
	return cc.lastConnectionError
}
