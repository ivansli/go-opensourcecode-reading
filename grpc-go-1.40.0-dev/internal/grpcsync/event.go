/*
 *
 * Copyright 2018 gRPC authors.
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

// Package grpcsync implements additional synchronization primitives built upon
// the sync package.
package grpcsync

import (
	"sync"
	"sync/atomic"
)

// Event represents a one-time event that may occur in the future.
// Event 代表未来某一刻有事件发生
type Event struct {
	fired int32
	c     chan struct{}
	o     sync.Once // 保证 fire、c只被执行一次操作
}

// Fire causes e to complete.  It is safe to call multiple times, and
// concurrently.  It returns true iff this call to Fire caused the signaling
// channel returned by Done to close.
//
// Fire 激发、引发
//
func (e *Event) Fire() bool {
	// ret 表示当前是否是真正执行了 Do() 方法
	// 只有第一个执行的协程才会返回true， 其他都是false
	ret := false
	e.o.Do(func() {
		atomic.StoreInt32(&e.fired, 1)
		close(e.c)
		ret = true
	})
	return ret
}

// Done returns a channel that will be closed when Fire is called.
//
// 返回一个当 Fire 被调用时会关闭的通道
func (e *Event) Done() <-chan struct{} {
	return e.c
}

// HasFired returns true if Fire has been called.
// 判断是否执行了 Fire
func (e *Event) HasFired() bool {
	return atomic.LoadInt32(&e.fired) == 1
}

// NewEvent returns a new, ready-to-use Event.
// 返回一个新的 Event
func NewEvent() *Event {
	return &Event{c: make(chan struct{})}
}
