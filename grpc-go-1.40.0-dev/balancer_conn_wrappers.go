/*
 *
 * Copyright 2017 gRPC authors.
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
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
)

// scStateUpdate contains the subConn and the new state it changed to.
type scStateUpdate struct {
	sc    balancer.SubConn
	state connectivity.State
	err   error
}

// ccBalancerWrapper is a wrapper on top of cc for balancers.
// It implements balancer.ClientConn interface.
type ccBalancerWrapper struct {
	cc         *ClientConn
	balancerMu sync.Mutex // synchronizes calls to the balancer
	balancer   balancer.Balancer
	updateCh   *buffer.Unbounded
	closed     *grpcsync.Event
	done       *grpcsync.Event

	mu       sync.Mutex
	subConns map[*acBalancerWrapper]struct{}
}

// 对负载均衡器 包装
func newCCBalancerWrapper(cc *ClientConn, b balancer.Builder, bopts balancer.BuildOptions) *ccBalancerWrapper {
	// 此时 cc *ClientConn 中已经包含了所有的可用地址信息
	ccb := &ccBalancerWrapper{
		cc:       cc,
		updateCh: buffer.NewUnbounded(), // 创建 updateCh 的 chan
		closed:   grpcsync.NewEvent(),
		done:     grpcsync.NewEvent(),
		subConns: make(map[*acBalancerWrapper]struct{}),
	}

	// 使用另一个协程来监听变化
	go ccb.watcher()

	// 使用 负载均衡构建器 来创建一个 负载均衡器
	// 如果是 roundrobin 在 balancer/roundrobin/roundrobin.go
	//
	// ccb 为负载均衡包装器， 被包含在 负载均衡器 中
	// 也就是说：负载均衡包装器 包含 负载均衡器， 负载均衡器 也包含 负载均衡包装器
	ccb.balancer = b.Build(ccb, bopts)
	return ccb
}

// watcher balancer functions sequentially, so the balancer can be implemented
// lock-free.
// watcher balancer 函数顺序，所以平衡器可以实现无锁。
func (ccb *ccBalancerWrapper) watcher() {
	for {
		select {
		case t := <-ccb.updateCh.Get():
			ccb.updateCh.Load()
			if ccb.closed.HasFired() {
				break
			}

			switch u := t.(type) {
			case *scStateUpdate:
				ccb.balancerMu.Lock()
				ccb.balancer.UpdateSubConnState(u.sc, balancer.SubConnState{ConnectivityState: u.state, ConnectionError: u.err})
				ccb.balancerMu.Unlock()
			case *acBalancerWrapper:
				ccb.mu.Lock()
				if ccb.subConns != nil {
					delete(ccb.subConns, u)
					ccb.cc.removeAddrConn(u.getAddrConn(), errConnDrain)
				}
				ccb.mu.Unlock()
			default:
				logger.Errorf("ccBalancerWrapper.watcher: unknown update %+v, type %T", t, t)
			}
		case <-ccb.closed.Done():
		}

		if ccb.closed.HasFired() {
			ccb.balancerMu.Lock()
			ccb.balancer.Close()
			ccb.balancerMu.Unlock()
			ccb.mu.Lock()
			scs := ccb.subConns
			ccb.subConns = nil
			ccb.mu.Unlock()
			ccb.UpdateState(balancer.State{ConnectivityState: connectivity.Connecting, Picker: nil})
			ccb.done.Fire()
			// Fire done before removing the addr conns.  We can safely unblock
			// ccb.close and allow the removeAddrConns to happen
			// asynchronously.
			for acbw := range scs {
				ccb.cc.removeAddrConn(acbw.getAddrConn(), errConnDrain)
			}
			return
		}
	}
}

func (ccb *ccBalancerWrapper) close() {
	ccb.closed.Fire()
	<-ccb.done.Done()
}

func (ccb *ccBalancerWrapper) handleSubConnStateChange(sc balancer.SubConn, s connectivity.State, err error) {
	// When updating addresses for a SubConn, if the address in use is not in
	// the new addresses, the old ac will be tearDown() and a new ac will be
	// created. tearDown() generates a state change with Shutdown state, we
	// don't want the balancer to receive this state change. So before
	// tearDown() on the old ac, ac.acbw (acWrapper) will be set to nil, and
	// this function will be called with (nil, Shutdown). We don't need to call
	// balancer method in this case.
	if sc == nil {
		return
	}
	ccb.updateCh.Put(&scStateUpdate{
		sc:    sc,
		state: s,
		err:   err,
	})
}

// 更新 服务端地址列表 并创建连接
//
// ccs *balancer.ClientConnState 包含解析出来的地址列表
func (ccb *ccBalancerWrapper) updateClientConnState(ccs *balancer.ClientConnState) error {
	// 需要对 负载均衡器操作 加锁
	ccb.balancerMu.Lock()
	defer ccb.balancerMu.Unlock()

	//////////////////////////////////////////////////////////////
	// ！！！由负载均衡器 ccb.balancer 来 建立连接
	// pick_first 在 pickfirst.go
	// roundrobin 在 balancer/base/balancer.go，roundrobin 对所有地址建立对应连接 并在invoke时使用
	//
	// ccb.balancer 为 实现了  balancer.Balancer 接口的对象
	//
	// ccb 是负载均衡器包装器，其中 balancer为  负载均衡器
	// 同时 balancer 负载均衡器，中也包含了 ccb 负载均衡器包装器
	////////////////////////////////////////////////////////////
	return ccb.balancer.UpdateClientConnState(*ccs)
}

func (ccb *ccBalancerWrapper) resolverError(err error) {
	ccb.balancerMu.Lock()
	ccb.balancer.ResolverError(err)
	ccb.balancerMu.Unlock()
}

// ！！！ 重要 ！！！
// 负载均衡器 会 对 第一次出现的地址创建一个 SubConn 对象
// 每一个服务端地址 都对应一个 SubConn 对象
//   type SubConn interface {
//         UpdateAddresses([]resolver.Address)
//         Connect()
//    }
func (ccb *ccBalancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if len(addrs) <= 0 {
		return nil, fmt.Errorf("grpc: cannot create SubConn with empty address list")
	}
	ccb.mu.Lock()
	defer ccb.mu.Unlock()

	if ccb.subConns == nil {
		return nil, fmt.Errorf("grpc: ClientConn balancer wrapper was closed")
	}

	// 对当前地址 创建一个  新的 addrConn
	ac, err := ccb.cc.newAddrConn(addrs, opts)
	if err != nil {
		return nil, err
	}

	// ac 的包装器
	acbw := &acBalancerWrapper{ac: ac}
	acbw.ac.mu.Lock()
	ac.acbw = acbw
	acbw.ac.mu.Unlock()
	ccb.subConns[acbw] = struct{}{}

	return acbw, nil
}

func (ccb *ccBalancerWrapper) RemoveSubConn(sc balancer.SubConn) {
	// The RemoveSubConn() is handled in the run() goroutine, to avoid deadlock
	// during switchBalancer() if the old balancer calls RemoveSubConn() in its
	// Close().
	ccb.updateCh.Put(sc)
}

func (ccb *ccBalancerWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	acbw.UpdateAddresses(addrs)
}

func (ccb *ccBalancerWrapper) UpdateState(s balancer.State) {
	ccb.mu.Lock()
	defer ccb.mu.Unlock()
	if ccb.subConns == nil {
		return
	}
	// Update picker before updating state.  Even though the ordering here does
	// not matter, it can lead to multiple calls of Pick in the common start-up
	// case where we wait for ready and then perform an RPC.  If the picker is
	// updated later, we could call the "connecting" picker when the state is
	// updated, and then call the "ready" picker after the picker gets updated.
	ccb.cc.blockingpicker.updatePicker(s.Picker)
	ccb.cc.csMgr.updateState(s.ConnectivityState)
}

func (ccb *ccBalancerWrapper) ResolveNow(o resolver.ResolveNowOptions) {
	ccb.cc.resolveNow(o)
}

func (ccb *ccBalancerWrapper) Target() string {
	return ccb.cc.target
}

// acBalancerWrapper is a wrapper on top of ac for balancers.
// It implements balancer.SubConn interface.
type acBalancerWrapper struct {
	mu sync.Mutex
	ac *addrConn
}

func (acbw *acBalancerWrapper) UpdateAddresses(addrs []resolver.Address) {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	if len(addrs) <= 0 {
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)
		return
	}
	if !acbw.ac.tryUpdateAddrs(addrs) {
		cc := acbw.ac.cc
		opts := acbw.ac.scopts
		acbw.ac.mu.Lock()
		// Set old ac.acbw to nil so the Shutdown state update will be ignored
		// by balancer.
		//
		// TODO(bar) the state transition could be wrong when tearDown() old ac
		// and creating new ac, fix the transition.
		acbw.ac.acbw = nil
		acbw.ac.mu.Unlock()
		acState := acbw.ac.getState()
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)

		if acState == connectivity.Shutdown {
			return
		}

		ac, err := cc.newAddrConn(addrs, opts)
		if err != nil {
			channelz.Warningf(logger, acbw.ac.channelzID, "acBalancerWrapper: UpdateAddresses: failed to newAddrConn: %v", err)
			return
		}
		acbw.ac = ac
		ac.mu.Lock()
		ac.acbw = acbw
		ac.mu.Unlock()
		if acState != connectivity.Idle {
			ac.connect()
		}
	}
}

// 对 acbw.ac 创建地址连接
func (acbw *acBalancerWrapper) Connect() {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()

	///////////////////////////////////////////////
	// ！！！重要
	// 对 ac 的地址建立一个可用的连接
	// ac 的地址连接状态 由一个新的协程来维持
	///////////////////////////////////////////////
	acbw.ac.connect()
}

func (acbw *acBalancerWrapper) getAddrConn() *addrConn {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	return acbw.ac
}
