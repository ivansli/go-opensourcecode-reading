package discovery

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
	"google.golang.org/grpc/resolver"
)

const name = "discovery"

// Option is builder option.
type Option func(o *builder)

// WithLogger with builder logger.
func WithLogger(logger log.Logger) Option {
	return func(o *builder) {
		o.logger = logger
	}
}

// 名称解析器构建器
type builder struct {
	discoverer registry.Discovery
	logger     log.Logger
}

// NewBuilder creates a builder which is used to factory registry resolvers.
func NewBuilder(d registry.Discovery, opts ...Option) resolver.Builder {
	b := &builder{
		discoverer: d,
		logger:     log.DefaultLogger,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (d *builder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	// 获取监听 chan
	// d.discoverer 是etcd时为 go-kratos/etcd@v0.1.0/registry/registry.go  New()
	w, err := d.discoverer.Watch(context.Background(), target.Endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 名称解析器
	r := &discoveryResolver{
		w:      w,
		cc:     cc,
		ctx:    ctx,
		cancel: cancel,
		log:    log.NewHelper(d.logger),
	}

	// 名称解析器 启动新的 goroutine 监听地址变化
	// 异步操作
	go r.watch()

	return r, nil
}

func (d *builder) Scheme() string {
	return name
}
