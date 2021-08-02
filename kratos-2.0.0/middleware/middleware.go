package middleware

import (
	"context"
)

// Handler defines the handler invoked by Middleware.
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

// Middleware is HTTP/gRPC transport middleware.
type Middleware func(Handler) Handler

// Chain returns a Middleware that specifies the chained handler for endpoint.
//
// 例如：
//
// func serviceMiddleware(handler middleware.Handler) middleware.Handler {
//	return func(ctx context.Context, req interface{}) (reply interface{}, err error) {
//		fmt.Println("service middleware in")
//
//		reply, err = handler(ctx, req)
//
//		fmt.Println("service middleware out")
//		return
//	}
// }
//
func Chain(m ...Middleware) Middleware {
	return func(next Handler) Handler {
		// 倒着开始封装，即 最后一个中间件在最内层，也是最后执行
		for i := len(m) - 1; i >= 0; i-- {
			// 每一个 m[i] 都是一个中间件方法
			// m[i](next) 含义是
			// 		把 中间件方法next 封装到中间件方法m[i]中，得到一个新的中间件方法
			// 		类似 俄罗斯套娃
			next = m[i](next)
		}

		return next
	}
}
