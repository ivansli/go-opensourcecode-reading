/*
 * Copyright 2019 gRPC authors.
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

// Package buffer provides an implementation of an unbounded buffer.
package buffer

import "sync"

// Unbounded is an implementation of an unbounded buffer which does not use
// extra goroutines. This is typically used for passing updates from one entity
// to another within gRPC.
// Unbounded 是一个不使用额外 goroutine 的无界缓冲区的实现
// 这通常用于在 gRPC 中将更新从一个实体传递到另一个实体
//
// All methods on this type are thread-safe and don't block on anything except
// the underlying mutex used for synchronization.
// 该类型上的所有方法都是线程安全的，除了用于同步的底层互斥锁外，不会阻塞任何东西
//
// Unbounded supports values of any type to be stored in it by using a channel
// of `interface{}`. This means that a call to Put() incurs an extra memory
// allocation, and also that users need a type assertion while reading. For
// performance critical code paths, using Unbounded is strongly discouraged and
// defining a new type specific implementation of this buffer is preferred. See
// internal/transport/transport.go for an example of this.
//
// Unbounded 支持使用 'interface{} '的通道来存储任何类型的值
// 这意味着对 Put() 的调用会导致额外的内存分配，而且用户在读取时还需要进行类型断言
// 对于性能关键的代码路径，强烈反对使用 Unbounded，最好定义该缓冲区的新类型特定实现
// 看 internal/transport/transport.go 例子
type Unbounded struct {
	c       chan interface{}
	mu      sync.Mutex

	// 把信息作为日志记录在 backlog 中
	backlog []interface{}
}

// NewUnbounded returns a new instance of Unbounded.
// 创建一个 新的 Unbounded 实例
func NewUnbounded() *Unbounded {
	return &Unbounded{c: make(chan interface{}, 1)}
}

// Put adds t to the unbounded buffer.
// put 添加 数据 t 到 unbounded 缓冲中
func (b *Unbounded) Put(t interface{}) {
	b.mu.Lock()
	if len(b.backlog) == 0 {
		select {
		case b.c <- t: // chan 有容量可以存储
			b.mu.Unlock()
			return // 发送到 chan 成功之后就退出
		default: // chan 已满，则走这里，通过下面逻辑存储到 backlog
		}
	}

	// t 添加到日志队列中
	b.backlog = append(b.backlog, t)
	b.mu.Unlock()
}

// Load sends the earliest buffered data, if any, onto the read channel
// returned by Get(). Users are expected to call this every time they read a
// value from the read channel.
//
// Load将最早的缓冲数据(如果有的话)发送到Get()返回的读通道
// 用户每次从读取通道读取值时都要调用这个函数
//
// 值得注意的时，每当消费掉 chan 中的一个数据时，就需要调用该方法一次，必须要调用！！！
func (b *Unbounded) Load() {
	b.mu.Lock()

	// backlog 有没有消费的数据，则取出来再次发送到 chan
	if len(b.backlog) > 0 {
		select {
		case b.c <- b.backlog[0]: // 取出来第一个，尝试发送到 chan
			b.backlog[0] = nil // backlog 第一个设置为 nil，有利于 垃圾回收
			b.backlog = b.backlog[1:] // 移除 第一个位置
		default: // 发送失败，走这里
		}
	}

	b.mu.Unlock()
}

// Get returns a read channel on which values added to the buffer, via Put(),
// are sent on.
//
// Upon reading a value from this channel, users are expected to call Load() to
// send the next buffered value onto the channel if there is any.
//
// 获取通道 chan
func (b *Unbounded) Get() <-chan interface{} {
	return b.c
}
