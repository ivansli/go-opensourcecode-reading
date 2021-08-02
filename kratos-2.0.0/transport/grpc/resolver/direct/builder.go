package direct

import (
	"strings"

	"google.golang.org/grpc/resolver"
)

// 注册名称解析器构建器
func init() {
	resolver.Register(NewBuilder())
}

type directBuilder struct{}

// NewBuilder creates a directBuilder which is used to factory direct resolvers.
// example:
//   direct://<authority>/127.0.0.1:9000,127.0.0.2:9000
func NewBuilder() resolver.Builder {
	return &directBuilder{}
}

func (d *directBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	var addrs []resolver.Address
	// 获取多个地址，并把每一个地址封装成 resolver.Address对象
	// 最后再把多个地址封装到 resolver.State 对象中
	for _, addr := range strings.Split(target.Endpoint, ",") {
		addrs = append(addrs, resolver.Address{Addr: addr})
	}

	// 更新到 客户端连接对象中，供在这些地址上建立连接
	cc.UpdateState(resolver.State{
		Addresses: addrs,
	})

	return newDirectResolver(), nil
}

func (d *directBuilder) Scheme() string {
	return "direct"
}
