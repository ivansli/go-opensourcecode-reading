package main

import (
	"context"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver"
	"strings"
	"time"
)

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

// 名称解析器的构造器对象
type Builder struct {
	client *clientv3.Client

	// 全局路由表快照, 非必要
	store map[string]map[string]struct{}
}

// 名称解析器的构建器
func NewBuilder(client *clientv3.Client) *Builder {
	return &Builder{
		client: client,
		store:  make(map[string]map[string]struct{}),
	}
}

// 构建 名称解析器
// ccResolverWrapper.resolver 中包含 etcdResolver
// etcdResolver.cc 中也包含 ccResolverWrapper
func (b *Builder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	b.store[target.Endpoint] = make(map[string]struct{})

	// 初始化 etcd resolver
	r := &etcdResolver{
		client: b.client,
		target: target,
		cc:     cc, // ccResolverWrapper
		store:  b.store[target.Endpoint],

		stopCh: make(chan struct{}, 1),
		rn:     make(chan struct{}, 1),      // 容量为1，防止阻塞。用于通知进行地址更新
		t:      time.NewTicker(defaultFreq), // 定时器，在 defaultFreq 内进行更新
	}

	// 需要进行一次全量更新服务地址
	r.ResolveNow(resolver.ResolveNowOptions{})

	// 开启后台更新 goroutine
	go r.start(context.Background())

	return r, nil
}

func (b *Builder) Scheme() string {
	return EtcdScheme
}

// etcd的 名称解析器 结构体
//
// 实现接口 Resolver
// type Resolver interface {
//	ResolveNow(ResolveNowOptions)
//	Close()
//}
type etcdResolver struct {
	client *clientv3.Client
	target resolver.Target

	// ccResolverWrapper 名称解析器包装对象
	// 包含有 grpc.ClientConn
	cc resolver.ClientConn

	// 很重要，存储解析的地址列表
	store  map[string]struct{}
	stopCh chan struct{}

	// rn channel is used by ResolveNow()
	// to force an immediate resolution of the target.
	rn chan struct{}
	t  *time.Ticker
}

// 很重要
// 开始监听是否需要更新
// 首先，该方法需要在新的 goroutine 中运行
// 其次，需要使用定时器，定时执行更新操作
func (r *etcdResolver) start(ctx context.Context) {
	target := r.target.Endpoint // serveName

	// etcd watch
	// r.client 为 etcd 客户端对象
	w := clientv3.NewWatcher(r.client)
	rch := w.Watch(ctx, target+"/", clientv3.WithPrefix()) // 获取一个监听 chan

	for {
		select {
		case <-r.rn: // 接收到更新通知，从etcd读取地址信息，进行更新
			// 在读取最新的数据之后 调用 r.updateTargetState() 进行更新
			// r.resolveNow() 是重新从etcd读取数据，再进行一次更新操作
			r.resolveNow()

		case <-r.t.C: // 定时器触发，进行一次更新
			// r.ResolveNow 是 发送一个消息到 r.rn 中
			// r.rn 接收到通知之后，立刻进行更新
			r.ResolveNow(resolver.ResolveNowOptions{})

		case <-r.stopCh: // 关闭了
			// etcd watch stop
			w.Close()
			// 定时器 stop
			r.t.Stop()
			return

		case wresp := <-rch: // 监听的etcd chan 发生了变化
			// etcd 事件
			for _, ev := range wresp.Events {
				switch ev.Type {
				case mvccpb.PUT: // 添加事件
					r.store[string(ev.Kv.Value)] = struct{}{}
				case mvccpb.DELETE: // 删除事件
					delete(r.store, strings.Replace(string(ev.Kv.Key), target+"/", "", 1))
				}
			}

			// 真正的进行连接客户端的更新的方法
			r.updateTargetState()
		}
	}
}

// 重新从etcd读取最新地址信息，再进行更新操作
func (r *etcdResolver) resolveNow() {
	target := r.target.Endpoint // 注册 etcd 时的 serveName
	resp, err := r.client.Get(context.Background(), target+"/", clientv3.WithPrefix())
	if err != nil {
		r.cc.ReportError(errors.Wrap(err, "get init endpoints"))
		return
	}

	// 获取所有的服务端地址
	for _, kv := range resp.Kvs {
		r.store[string(kv.Value)] = struct{}{}
	}

	// 开始更新地址到 负载均衡器 中
	r.updateTargetState()
}

// 更新操作
func (r *etcdResolver) updateTargetState() {
	addrs := make([]resolver.Address, len(r.store))

	i := 0
	for k := range r.store {
		addrs[i] = resolver.Address{Addr: k}
		i++
	}

	// r.cc ccResolverWrapper 名称解析器包装对象
	// 更新客户端的地址信息
	r.cc.UpdateState(resolver.State{Addresses: addrs})
}

// 会并发调用, 所以这里防止同时多次全量刷新
func (r *etcdResolver) ResolveNow(o resolver.ResolveNowOptions) {
	select {
	case r.rn <- struct{}{}: // 向chan发送信号，提示进行更新
	default: // 一定要加这个 default，防止阻塞在 r.rn 上
	}
}

// 关闭操作
func (r *etcdResolver) Close() {
	r.t.Stop()      // 关闭定时器
	close(r.stopCh) // 关闭通知通道
}
