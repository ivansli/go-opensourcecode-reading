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
	"context"
	"io"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/status"
)

// pickerWrapper is a wrapper of balancer.Picker. It blocks on certain pick
// actions and unblock when there's a picker update.
//
// pickerWrapper是balancer.Picker的包装器
// 它在特定的选择操作上阻塞，当有选择器更新时解除阻塞。
type pickerWrapper struct {
	mu   sync.Mutex
	done bool

	blockingCh chan struct{}

	// 负载均衡器的 选择器
	picker     balancer.Picker
}

// 获取 负载均衡器的 选择器的包装对象
func newPickerWrapper() *pickerWrapper {
	return &pickerWrapper{blockingCh: make(chan struct{})}
}

// updatePicker is called by UpdateBalancerState. It unblocks all blocked pick.
// 更新 负载均衡器的 选择器
func (pw *pickerWrapper) updatePicker(p balancer.Picker) {
	pw.mu.Lock()
	if pw.done {
		pw.mu.Unlock()
		return
	}

	pw.picker = p
	// pw.blockingCh should never be nil.
	close(pw.blockingCh)

	pw.blockingCh = make(chan struct{})
	pw.mu.Unlock()
}

func doneChannelzWrapper(acw *acBalancerWrapper, done func(balancer.DoneInfo)) func(balancer.DoneInfo) {
	acw.mu.Lock()
	ac := acw.ac
	acw.mu.Unlock()
	ac.incrCallsStarted()
	return func(b balancer.DoneInfo) {
		if b.Err != nil && b.Err != io.EOF {
			ac.incrCallsFailed()
		} else {
			ac.incrCallsSucceeded()
		}
		if done != nil {
			done(b)
		}
	}
}

// pick returns the transport that will be used for the RPC.
// 负载均衡器的选择方法 用来选择一个可用的连接 返回将用于RPC的传输
//
// It may block in the following cases:
// - there's no picker
// - the current picker returns ErrNoSubConnAvailable
// - the current picker returns other errors and failfast is false.
// - the subConn returned by the current picker is not READY
// When one of these situations happens, pick blocks until the picker gets updated.
func (pw *pickerWrapper) pick(ctx context.Context, failfast bool, info balancer.PickInfo) (transport.ClientTransport, func(balancer.DoneInfo), error) {
	var ch chan struct{}

	var lastPickErr error
	for {
		pw.mu.Lock()
		if pw.done {
			pw.mu.Unlock()
			return nil, nil, ErrClientConnClosing
		}

		if pw.picker == nil {
			ch = pw.blockingCh
		}

		if ch == pw.blockingCh {
			// This could happen when either:
			// - pw.picker is nil (the previous if condition), or
			// - has called pick on the current picker.
			pw.mu.Unlock()
			select {
			case <-ctx.Done():
				var errStr string
				if lastPickErr != nil {
					errStr = "latest balancer error: " + lastPickErr.Error()
				} else {
					errStr = ctx.Err().Error()
				}

				switch ctx.Err() {
				case context.DeadlineExceeded:
					return nil, nil, status.Error(codes.DeadlineExceeded, errStr)
				case context.Canceled:
					return nil, nil, status.Error(codes.Canceled, errStr)
				}
			case <-ch:
			}

			continue
		}

		ch = pw.blockingCh

		// 负载均衡选择器的选择方法
		p := pw.picker
		pw.mu.Unlock()

		/////////////////////////////////////
		// TODO (追源码)
		//  ！！！重要 ！！！
		//  负载均衡选择器选择一个可用连接
		/////////////////////////////////////

		// 例如 roundrobin 的 pick 方法
		// balancer/roundrobin/roundrobin.go
		// pickResult 为获取的一个连接
		pickResult, err := p.Pick(info)

		if err != nil {
			if err == balancer.ErrNoSubConnAvailable {
				continue
			}
			if _, ok := status.FromError(err); ok {
				// Status error: end the RPC unconditionally with this status.
				return nil, nil, err
			}
			// For all other errors, wait for ready RPCs should block and other
			// RPCs should fail with unavailable.
			//
			// 如果不是快速失败，则继续重试，否则立刻返回错误
			if !failfast {
				lastPickErr = err
				continue
			}

			return nil, nil, status.Error(codes.Unavailable, err.Error())
		}

		// 断言
		acw, ok := pickResult.SubConn.(*acBalancerWrapper)
		if !ok {
			logger.Error("subconn returned from pick is not *acBalancerWrapper")
			continue
		}

		// 连接已经准备就绪
		// acw.getAddrConn() 等于 addrconn
		if t := acw.getAddrConn().getReadyTransport(); t != nil {
			if channelz.IsOn() {
				return t, doneChannelzWrapper(acw, pickResult.Done), nil
			}

			// t 就是准备好的连接
			// pickResult.Done 是 func(DoneInfo)
			return t, pickResult.Done, nil
		}

		if pickResult.Done != nil {
			// Calling done with nil error, no bytes sent and no bytes received.
			// DoneInfo with default value works.
			pickResult.Done(balancer.DoneInfo{})
		}

		logger.Infof("blockingPicker: the picked transport is not ready, loop back to repick")
		// If ok == false, ac.state is not READY.
		// A valid picker always returns READY subConn. This means the state of ac
		// just changed, and picker will be updated shortly.
		// continue back to the beginning of the for loop to repick.
	}
}

func (pw *pickerWrapper) close() {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if pw.done {
		return
	}
	pw.done = true
	close(pw.blockingCh)
}
