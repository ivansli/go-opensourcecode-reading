package grpc

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/go-kratos/kratos/v2/transport/grpc/resolver/discovery"

	// init resolver
	_ "github.com/go-kratos/kratos/v2/transport/grpc/resolver/direct"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"
	grpcmd "google.golang.org/grpc/metadata"
)

// ClientOption is gRPC client option.
type ClientOption func(o *clientOptions)

// WithEndpoint with client endpoint.
func WithEndpoint(endpoint string) ClientOption {
	return func(o *clientOptions) {
		o.endpoint = endpoint
	}
}

// WithTimeout with client timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.timeout = timeout
	}
}

// WithMiddleware with client middleware.
func WithMiddleware(m ...middleware.Middleware) ClientOption {
	return func(o *clientOptions) {
		o.middleware = m
	}
}

// WithDiscovery with client discovery.
func WithDiscovery(d registry.Discovery) ClientOption {
	return func(o *clientOptions) {
		o.discovery = d
	}
}

// WithUnaryInterceptor returns a DialOption that specifies the interceptor for unary RPCs.
func WithUnaryInterceptor(in ...grpc.UnaryClientInterceptor) ClientOption {
	return func(o *clientOptions) {
		o.ints = in
	}
}

// WithOptions with gRPC options.
func WithOptions(opts ...grpc.DialOption) ClientOption {
	return func(o *clientOptions) {
		o.grpcOpts = opts
	}
}

// clientOptions is gRPC Client
type clientOptions struct {
	endpoint   string
	timeout    time.Duration
	discovery  registry.Discovery
	middleware []middleware.Middleware
	ints       []grpc.UnaryClientInterceptor
	grpcOpts   []grpc.DialOption
}

// Dial returns a GRPC connection.
func Dial(ctx context.Context, opts ...ClientOption) (*grpc.ClientConn, error) {
	return dial(ctx, false, opts...)
}

// DialInsecure returns an insecure GRPC connection.
func DialInsecure(ctx context.Context, opts ...ClientOption) (*grpc.ClientConn, error) {
	return dial(ctx, true, opts...)
}

func dial(ctx context.Context, insecure bool, opts ...ClientOption) (*grpc.ClientConn, error) {
	options := clientOptions{
		timeout: 500 * time.Millisecond,
	}
	// 设置 clientOptions 信息
	for _, o := range opts {
		o(&options)
	}

	// 把 中间件 添加到一元拦截器
	var ints = []grpc.UnaryClientInterceptor{
		unaryClientInterceptor(options.middleware, options.timeout),
	}
	if len(options.ints) > 0 {
		ints = append(ints, options.ints...)
	}

	var grpcOpts = []grpc.DialOption{
		// 使用的负载均衡器 为 roundrobin
		// 在引入 roundrobin 包的时候，就已经把该 负载均衡器进行了注册
		grpc.WithBalancerName(roundrobin.Name),
		grpc.WithChainUnaryInterceptor(ints...),
	}

	// 服务发现
	if options.discovery != nil {
		// grpc.WithResolvers() 指定名称解析器构建器
		// 这里的 构建器为 kratos-2.0.0/transport/grpc/resolver/discovery/builder.go  builder
		// 添加 名称解析器的构建器
		grpcOpts = append(grpcOpts, grpc.WithResolvers(discovery.NewBuilder(options.discovery)))
	}

	// 非安全连接
	if insecure {
		grpcOpts = append(grpcOpts, grpc.WithInsecure())
	}

	if len(options.grpcOpts) > 0 {
		grpcOpts = append(grpcOpts, options.grpcOpts...)
	}

	return grpc.DialContext(ctx, options.endpoint, grpcOpts...)
}

// 把 中间件 封装成拦截器
func unaryClientInterceptor(ms []middleware.Middleware, timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 把 Transport 封装到 ctx 中
		ctx = transport.NewClientContext(ctx, &Transport{
			endpoint:  cc.Target(),
			operation: method,
			header:    headerCarrier{},
		})

		// 超时时间的控制
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		// 把最终请求服务端的方法封装成一个 中间件 类型的方法
		// 并把该 中间件 类型的方法放在整个 中间件洋葱模型 的最内层
		h := func(ctx context.Context, req interface{}) (interface{}, error) {
			if tr, ok := transport.FromClientContext(ctx); ok {
				keys := tr.Header().Keys()
				keyvals := make([]string, 0, len(keys))
				for _, k := range keys {
					keyvals = append(keyvals, k, tr.Header().Get(k))
				}
				ctx = grpcmd.AppendToOutgoingContext(ctx, keyvals...)
			}

			// invoker(ctx, method, req, reply, cc, opts...) 是最终执行请求服务端的方法
			return reply, invoker(ctx, method, req, reply, cc, opts...)
		}

		// 把多个中间件方法 封装成 一个 Handler
		// 套洋葱模型，在最核心的一层里面包含最终的请求方法 invoker
		if len(ms) > 0 {
			h = middleware.Chain(ms...)(h)
		}

		_, err := h(ctx, req)

		// 一元拦截器 类型的方法 最终只返回一个 error 类型
		return err
	}
}
