package registry

import (
	"context"

	"github.com/go-kratos/kratos/v2/registry"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	_ registry.Watcher = &watcher{}
)

type watcher struct {
	key       string
	ctx       context.Context
	cancel    context.CancelFunc
	watchChan clientv3.WatchChan
	watcher   clientv3.Watcher
	kv        clientv3.KV
	ch        clientv3.WatchChan
}

func newWatcher(ctx context.Context, key string, client *clientv3.Client) *watcher {
	w := &watcher{
		key:     key,
		watcher: clientv3.NewWatcher(client),
		kv:      clientv3.NewKV(client),
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.watchChan = w.watcher.Watch(w.ctx, key, clientv3.WithPrefix(), clientv3.WithRev(0))

	//RequestProgress请求在所有监视通道中发送一个进度通知响应。
	w.watcher.RequestProgress(context.Background())
	return w
}

// 通过监听在 watchChan 获取最新的 服务地址信息
func (w *watcher) Next() ([]*registry.ServiceInstance, error) {
	for {
		select {
		// 超时或取消
		case <-w.ctx.Done():
			return nil, w.ctx.Err()
		// 监听的 chan 发生变化 或 通道关闭
		case <-w.watchChan:
		}
		
		// 获取所有注册的服务地址信息
		resp, err := w.kv.Get(w.ctx, w.key, clientv3.WithPrefix())
		if err != nil {
			return nil, err
		}

		var items []*registry.ServiceInstance
		for _, kv := range resp.Kvs {
			// 反序列化
			si, err := unmarshal(kv.Value)
			if err != nil {
				return nil, err
			}

			items = append(items, si)
		}
		return items, nil
	}
}

func (w *watcher) Stop() error {
	w.cancel()
	return w.watcher.Close()
}
