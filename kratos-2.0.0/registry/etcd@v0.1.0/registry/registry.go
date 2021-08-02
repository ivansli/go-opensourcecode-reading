package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	_ registry.Registrar = &Registry{}
	_ registry.Discovery = &Registry{}
)

// Option is etcd registry option.
type Option func(o *options)

type options struct {
	ctx       context.Context
	namespace string
	ttl       time.Duration
}

// Context with registry context.
func Context(ctx context.Context) Option {
	return func(o *options) { o.ctx = ctx }
}

// Namespace with registry namespance.
func Namespace(ns string) Option {
	return func(o *options) { o.namespace = ns }
}

// RegisterTTL with register ttl.
func RegisterTTL(ttl time.Duration) Option {
	return func(o *options) { o.ttl = ttl }
}

// Registry is etcd registry.
type Registry struct {
	opts   *options
	client *clientv3.Client
	kv     clientv3.KV
	lease  clientv3.Lease
}

// New creates etcd registry
//
// 创建 etcd registry
func New(client *clientv3.Client, opts ...Option) (r *Registry) {
	options := &options{
		ctx:       context.Background(),
		namespace: "/microservices",
		ttl:       time.Second * 15,
	}
	for _, o := range opts {
		o(options)
	}

	return &Registry{
		opts:   options, // 配置项参数，包括 ctx、ttl、以及 地址
		client: client,
		kv:     clientv3.NewKV(client),
	}
}

// Register the registration.
// Register 登记，相当于服务注册到etcd服务
func (r *Registry) Register(ctx context.Context, service *registry.ServiceInstance) error {
	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)
	value, err := marshal(service)
	if err != nil {
		return err
	}
	if r.lease != nil {
		r.lease.Close()
	}
	r.lease = clientv3.NewLease(r.client)
	grant, err := r.lease.Grant(ctx, int64(r.opts.ttl.Seconds()))
	if err != nil {
		return err
	}

	// 注册到etcd
	_, err = r.client.Put(ctx, key, value, clientv3.WithLease(grant.ID))
	if err != nil {
		return err
	}

	// 保持长连接
	hb, err := r.client.KeepAlive(ctx, clientv3.LeaseID(grant.ID))
	if err != nil {
		return err
	}

	// etcd 长连接 需要在另一个goroutine中执行 <-hb
	go func() {
		for {
			select {
			case _, ok := <-hb:
				if !ok {
					return
				}

			//	超时 或者 取消
			case <-r.opts.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Deregister the registration.
// 注销注册的服务信息
func (r *Registry) Deregister(ctx context.Context, service *registry.ServiceInstance) error {
	defer func() {
		if r.lease != nil {
			r.lease.Close()
		}
	}()

	key := fmt.Sprintf("%s/%s/%s", r.opts.namespace, service.Name, service.ID)

	// 删除对应 key
	_, err := r.client.Delete(ctx, key)
	return err
}

// GetService return the service instances in memory according to the service name.
// 获取虽有注册的服务信息，相当于服务发现
func (r *Registry) GetService(ctx context.Context, name string) ([]*registry.ServiceInstance, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	resp, err := r.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	var items []*registry.ServiceInstance
	for _, kv := range resp.Kvs {
		si, err := unmarshal(kv.Value)
		if err != nil {
			return nil, err
		}
		items = append(items, si)
	}
	return items, nil
}

// Watch creates a watcher according to the service name.
// 监听服务变化
func (r *Registry) Watch(ctx context.Context, name string) (registry.Watcher, error) {
	key := fmt.Sprintf("%s/%s", r.opts.namespace, name)
	return newWatcher(ctx, key, r.client), nil
}
