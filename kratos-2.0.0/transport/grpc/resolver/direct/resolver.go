package direct

import "google.golang.org/grpc/resolver"

type directResolver struct{}

// 名称解析器
//
// 需要实现下面接口：
// type Resolver interface {
//    ResolveNow(ResolveNowOptions)
//    Close()
// }
func newDirectResolver() resolver.Resolver {
	return &directResolver{}
}

func (r *directResolver) Close() {
}

func (r *directResolver) ResolveNow(options resolver.ResolveNowOptions) {
}
