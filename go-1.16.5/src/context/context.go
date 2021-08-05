// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package context defines the Context type, which carries deadlines,
// cancellation signals, and other request-scoped values across API boundaries
// and between processes.
//
// Incoming requests to a server should create a Context, and outgoing
// calls to servers should accept a Context. The chain of function
// calls between them must propagate the Context, optionally replacing
// it with a derived Context created using WithCancel, WithDeadline,
// WithTimeout, or WithValue. When a Context is canceled, all
// Contexts derived from it are also canceled.
//
// The WithCancel, WithDeadline, and WithTimeout functions take a
// Context (the parent) and return a derived Context (the child) and a
// CancelFunc. Calling the CancelFunc cancels the child and its
// children, removes the parent's reference to the child, and stops
// any associated timers. Failing to call the CancelFunc leaks the
// child and its children until the parent is canceled or the timer
// fires. The go vet tool checks that CancelFuncs are used on all
// control-flow paths.
//
// Programs that use Contexts should follow these rules to keep interfaces
// consistent across packages and enable static analysis tools to check context
// propagation:
//
// Do not store Contexts inside a struct type; instead, pass a Context
// explicitly to each function that needs it. The Context should be the first
// parameter, typically named ctx:
//
// 	func DoSomething(ctx context.Context, arg Arg) error {
// 		// ... use ctx ...
// 	}
//
// Do not pass a nil Context, even if a function permits it. Pass context.TODO
// if you are unsure about which Context to use.
//
// Use context Values only for request-scoped data that transits processes and
// APIs, not for passing optional parameters to functions.
//
// The same Context may be passed to functions running in different goroutines;
// Contexts are safe for simultaneous use by multiple goroutines.
//
// See https://blog.golang.org/context for example code for a server that uses
// Contexts.
package context

import (
	"errors"
	"internal/reflectlite"
	"sync"
	"sync/atomic"
	"time"
)

// A Context carries a deadline, a cancellation signal, and other values across
// API boundaries.
//
// Context's methods may be called by multiple goroutines simultaneously.
type Context interface {
	// Deadline returns the time when work done on behalf of this context
	// should be canceled. Deadline returns ok==false when no deadline is
	// set. Successive calls to Deadline return the same results.
	Deadline() (deadline time.Time, ok bool)

	// Done returns a channel that's closed when work done on behalf of this
	// context should be canceled. Done may return nil if this context can
	// never be canceled. Successive calls to Done return the same value.
	// The close of the Done channel may happen asynchronously,
	// after the cancel function returns.
	//
	// WithCancel arranges for Done to be closed when cancel is called;
	// WithDeadline arranges for Done to be closed when the deadline
	// expires; WithTimeout arranges for Done to be closed when the timeout
	// elapses.
	//
	// Done is provided for use in select statements:
	//
	//  // Stream generates values with DoSomething and sends them to out
	//  // until DoSomething returns an error or ctx.Done is closed.
	//  func Stream(ctx context.Context, out chan<- Value) error {
	//  	for {
	//  		v, err := DoSomething(ctx)
	//  		if err != nil {
	//  			return err
	//  		}
	//  		select {
	//  		case <-ctx.Done():
	//  			return ctx.Err()
	//  		case out <- v:
	//  		}
	//  	}
	//  }
	//
	// See https://blog.golang.org/pipelines for more examples of how to use
	// a Done channel for cancellation.
	Done() <-chan struct{}

	// If Done is not yet closed, Err returns nil.
	// If Done is closed, Err returns a non-nil error explaining why:
	// Canceled if the context was canceled
	// or DeadlineExceeded if the context's deadline passed.
	// After Err returns a non-nil error, successive calls to Err return the same error.
	Err() error

	// Value returns the value associated with this context for key, or nil
	// if no value is associated with key. Successive calls to Value with
	// the same key returns the same result.
	//
	// Use context values only for request-scoped data that transits
	// processes and API boundaries, not for passing optional parameters to
	// functions.
	//
	// A key identifies a specific value in a Context. Functions that wish
	// to store values in Context typically allocate a key in a global
	// variable then use that key as the argument to context.WithValue and
	// Context.Value. A key can be any type that supports equality;
	// packages should define keys as an unexported type to avoid
	// collisions.
	//
	// Packages that define a Context key should provide type-safe accessors
	// for the values stored using that key:
	//
	// 	// Package user defines a User type that's stored in Contexts.
	// 	package user
	//
	// 	import "context"
	//
	// 	// User is the type of value stored in the Contexts.
	// 	type User struct {...}
	//
	// 	// key is an unexported type for keys defined in this package.
	// 	// This prevents collisions with keys defined in other packages.
	// 	type key int
	//
	// 	// userKey is the key for user.User values in Contexts. It is
	// 	// unexported; clients use user.NewContext and user.FromContext
	// 	// instead of using this key directly.
	// 	var userKey key
	//
	// 	// NewContext returns a new Context that carries value u.
	// 	func NewContext(ctx context.Context, u *User) context.Context {
	// 		return context.WithValue(ctx, userKey, u)
	// 	}
	//
	// 	// FromContext returns the User value stored in ctx, if any.
	// 	func FromContext(ctx context.Context) (*User, bool) {
	// 		u, ok := ctx.Value(userKey).(*User)
	// 		return u, ok
	// 	}
	Value(key interface{}) interface{}
}

// Canceled is the error returned by Context.Err when the context is canceled.
// 被取消的错误信息
var Canceled = errors.New("context canceled")

// DeadlineExceeded is the error returned by Context.Err when the context's
// deadline passes.
var DeadlineExceeded error = deadlineExceededError{}

type deadlineExceededError struct{}

func (deadlineExceededError) Error() string   { return "context deadline exceeded" }
func (deadlineExceededError) Timeout() bool   { return true }
func (deadlineExceededError) Temporary() bool { return true }

// An emptyCtx is never canceled, has no values, and has no deadline. It is not
// struct{}, since vars of this type must have distinct addresses.
//
// 一个emptyCtx永远不会被取消，没有值，没有截止日期
// 它不是 struct{}，因为这种类型的变量必须有不同的地址
// (因为空的 struct，地址都一样，所以不使用空的 struct)
type emptyCtx int

func (*emptyCtx) Deadline() (deadline time.Time, ok bool) {
	return
}

func (*emptyCtx) Done() <-chan struct{} {
	return nil
}

func (*emptyCtx) Err() error {
	return nil
}

func (*emptyCtx) Value(key interface{}) interface{} {
	return nil
}

func (e *emptyCtx) String() string {
	switch e {
	case background:
		return "context.Background"
	case todo:
		return "context.TODO"
	}
	return "unknown empty Context"
}

var (
	background = new(emptyCtx)
	todo       = new(emptyCtx)
)

// Background returns a non-nil, empty Context. It is never canceled, has no
// values, and has no deadline. It is typically used by the main function,
// initialization, and tests, and as the top-level Context for incoming
// requests.
func Background() Context {
	return background
}

// TODO returns a non-nil, empty Context. Code should use context.TODO when
// it's unclear which Context to use or it is not yet available (because the
// surrounding function has not yet been extended to accept a Context
// parameter).
func TODO() Context {
	return todo
}

// A CancelFunc tells an operation to abandon its work.
// A CancelFunc does not wait for the work to stop.
// A CancelFunc may be called by multiple goroutines simultaneously.
// After the first call, subsequent calls to a CancelFunc do nothing.
type CancelFunc func()

// WithCancel returns a copy of parent with a new Done channel. The returned
// context's Done channel is closed when the returned cancel function is called
// or when the parent context's Done channel is closed, whichever happens first.
// WithCancel返回一个带有新Done通道的父类
// 当返回的cancel函数被调用或父上下文的Done通道被关闭时
// 返回上下文的Done通道将被关闭，以最先发生的为准
//
//
// Canceling this context releases resources associated with it, so code should
// call cancel as soon as the operations running in this Context complete.
// 取消此上下文将释放与之关联的资源
// 因此代码应该在此上下文中运行的操作完成后立即调用cancel
func WithCancel(parent Context) (ctx Context, cancel CancelFunc) {
	if parent == nil {
		panic("cannot create context from nil parent")
	}

	// 创建可用于取消的ctx对象
	c := newCancelCtx(parent)

	// 1. 当 parent节点 不在时，安排 child节点 取消
	// 2. 把子节点 添加到 父节点的孩子节点map 中
	propagateCancel(parent, &c)

	return &c, func() { c.cancel(true, Canceled) }
}

// newCancelCtx returns an initialized cancelCtx.
// 初始化 cancelCtx
func newCancelCtx(parent Context) cancelCtx {
	return cancelCtx{Context: parent}
}

// goroutines counts the number of goroutines ever created; for testing.
// Goroutines计算已经创建的Goroutines的数量
var goroutines int32

// propagateCancel arranges for child to be canceled when parent is.
//
// 与父节点进行一些关联操作
// 当 parent节点 不在时，安排 child节点 取消
func propagateCancel(parent Context, child canceler) {
	// 获取 parent 的 chan
	done := parent.Done()

	// parent 没有 chan，直接返回
	// TO-DO / Background / WithValue 创建的 ctx 中 chan 为 nil
	if done == nil {
		return // parent is never canceled
	}

	select {
	// parent 已经取消或者超时
	// 则子节点也取消，并把父节点的错误 赋给 子节点，作为自己点的错误
	case <-done:
		// parent is already canceled
		//
		// 由于还没跟 父节点绑定 所以 第一个参数为 false
		child.cancel(false, parent.Err())
		return
	default:
	}

	// 从传入的 parent 对象开始，依次往上找到一个最近的 还没有取消 并且 可以被 cancel 的对象
	//
	// ok == true 表示找到了 具有 cancel 的父节点
	if p, ok := parentCancelCtx(parent); ok {
		// 找到了父节点中可以 cancel 的对象
		p.mu.Lock()

		// 父节点已经取消
		if p.err != nil {
			// parent has already been canceled
			// 子节点也取消
			child.cancel(false, p.err)
		} else {
			// 父节点正常
			// 把 子节点加入到 父节点的 children map中
			if p.children == nil {
				p.children = make(map[canceler]struct{})
			}

			p.children[child] = struct{}{}
		}
		p.mu.Unlock()
	} else {
		// 这里有几种情况
		// 1. 父节点已经取消
		// 2. 父节点没有 cancel
		// 3. 父节点是 第三方 的类型，此时需单独 监听 该父节点状态，即 启动新的协程

		atomic.AddInt32(&goroutines, +1)

		// ！！！
		// 启动新的 goroutine 来监听
		go func() {
			select {
			// 父节点取消，则取消子节点
			case <-parent.Done():
				child.cancel(false, parent.Err())
			// 等待子节点 超时 或者 取消
			case <-child.Done():
			}
		}()
	}
}

// &cancelCtxKey is the key that a cancelCtx returns itself for.
//
// &cancelctxkey 是cancelCtx返回的键
var cancelCtxKey int

// parentCancelCtx returns the underlying *cancelCtx for parent.
// It does this by looking up parent.Value(&cancelCtxKey) to find
// the innermost enclosing *cancelCtx and then checking whether
// parent.Done() matches that *cancelCtx. (If not, the *cancelCtx
// has been wrapped in a custom implementation providing a
// different done channel, in which case we should not bypass it.)
//
// parentCancelCtx返回parent的基础*cancelCtx
// 它通过查找parent.Value(&cancelCtxKey)来找到最内层的*cancelCtx
// 然后检查parent.Done()是否匹配*cancelCtx
// (如果没有，*cancelCtx已经被包装在一个自定义实现中
// 提供了一个不同的完成通道，在这种情况下，我们不应该绕过它。)
func parentCancelCtx(parent Context) (*cancelCtx, bool) {
	// 调用回溯链中第一个实现了 Done() 的实例(第三方Context类/cancelCtx)
	//
	// 如果是结构体内嵌的话，此时也是递归找到第一个含有 Done() 方法的 ctx 的 chan
	done := parent.Done()

	// done == closedchan 说明通道已经关闭，即 父节点已经取消
	// done == nil 说明父节点是根节点
	if done == closedchan || done == nil {
		return nil, false
	}

	// 从传入的 parent 对象开始，依次往上找到一个最近的可以被 cancel 的对象
	// 即cancelCtx 或者 timerCtx
	//
	// parent.Value 是一个递归的过程
	p, ok := parent.Value(&cancelCtxKey).(*cancelCtx)
	if !ok {
		return nil, false
	}

	p.mu.Lock()

	// 在 valueCtx、cancelCtx、timerCtx 中
	// 只有 cancelCtx 直接  （valueCtx 和 timerCtx 都是通过嵌入实现，
	// 调用该方法会直接转发到 cancelCtx 或者 emptyCtx ）  实现了非空 Done() 方法
	// 因此 done := parent.Done() 会返回第一个祖先 cancelCtx 中的 done channel
	// 但如果 Context 树中有第三方实现的 Context 接口的实例时
	// parent.Done() 就有可能返回其他 channel
	//
	// 因此，如果 p.done != done
	// 说明在回溯链中遇到的第一个实现非空 Done() Context 是第三方 Context
	// 而非 cancelCtx

	// done 是本函数最开始获取的第一个具有 done() 方法 的父节点 ctx 的 chan
	// 这里的 p 也是通过递归获取到的父节点中第一个具有 cancel 的ctx
	// 判断 两个是否相等，相等则表示找到的是同一个 父节点
	ok = p.done == done

	p.mu.Unlock()

	// 说明回溯链中第一个实现 Done() 的实例不是 cancelCtx 的实例
	if !ok {
		return nil, false
	}
	return p, true
}

// removeChild removes a context from its parent.
// 从父节点移除自己
func removeChild(parent Context, child canceler) {
	p, ok := parentCancelCtx(parent)
	if !ok {
		return
	}

	p.mu.Lock()
	if p.children != nil {
		delete(p.children, child)
	}

	p.mu.Unlock()
}

// A canceler is a context type that can be canceled directly. The
// implementations are *cancelCtx and *timerCtx.
type canceler interface {
	cancel(removeFromParent bool, err error)
	Done() <-chan struct{}
}

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// A cancelCtx can be canceled. When canceled, it also cancels any children
// that implement canceler.
type cancelCtx struct {
	Context

	mu       sync.Mutex            // protects following fields
	done     chan struct{}         // created lazily, closed by first cancel call
	children map[canceler]struct{} // set to nil by the first cancel call
	err      error                 // set to non-nil by the first cancel call
}

func (c *cancelCtx) Value(key interface{}) interface{} {
	// 遇到特殊 key：cancelCtxKey 时，会返回自身
	// 这个其实是复用了 Value 函数的回溯逻辑
	// 从而在 Context 树回溯链中遍历时
	// 可以找到给定 Context 的第一个祖先 cancelCtx 实例
	if key == &cancelCtxKey {
		return c
	}

	// Value 函数的回溯逻辑
	return c.Context.Value(key)
}

// 获取用于通知的chan
// 如果不存在，则创建
func (c *cancelCtx) Done() <-chan struct{} {
	// 加锁，防止并发
	c.mu.Lock()
	if c.done == nil {
		c.done = make(chan struct{})
	}
	d := c.done
	c.mu.Unlock()

	return d
}

func (c *cancelCtx) Err() error {
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	return err
}

type stringer interface {
	String() string
}

func contextName(c Context) string {
	if s, ok := c.(stringer); ok {
		return s.String()
	}
	return reflectlite.TypeOf(c).String()
}

func (c *cancelCtx) String() string {
	return contextName(c.Context) + ".WithCancel"
}

// cancel closes c.done, cancels each of c's children, and, if
// removeFromParent is true, removes c from its parent's children.
//
// 1. 关闭通道
// 2. 取消所有子节点
// 3. 把自己从父节点中移除
func (c *cancelCtx) cancel(removeFromParent bool, err error) {
	if err == nil {
		panic("context: internal error: missing cancel error")
	}

	c.mu.Lock()

	// ！！！ 很重要，防止多次重复执行取消操作
	// 根据err是否为nil 判断是否已经取消
	// 在 WithTime() 中，有两个地方会触发取消操作
	// 1. time.AfterFunc
	// 2. WithTime() 返回的第二个参数 cancelFunc
	// 如果这2处都取执行cancel，不进行防护，会执行2次下面的 close(c.down)，会panic
	// ！！！由于加的有锁，所以只有其中一个能够真正的执行成功
	if c.err != nil {
		c.mu.Unlock()
		return // already canceled
	}

	// 把错误信息赋值给当前ctx
	c.err = err

	// 用于通知的通道不存在，则直接赋值一个已经关闭的通道
	// 懒初始化
	if c.done == nil {
		c.done = closedchan
	} else {
		close(c.done)
	}

	// 把当前节点的所有节点取消
	for child := range c.children {
		// NOTE: acquiring the child's lock while holding parent's lock.
		child.cancel(false, err)
	}
	c.children = nil

	c.mu.Unlock()

	// 从父节点的 map 中移除当前节点
	if removeFromParent {
		removeChild(c.Context, c)
	}
}

// WithDeadline returns a copy of the parent context with the deadline adjusted
// to be no later than d. If the parent's deadline is already earlier than d,
// WithDeadline(parent, d) is semantically equivalent to parent. The returned
// context's Done channel is closed when the deadline expires, when the returned
// cancel function is called, or when the parent context's Done channel is
// closed, whichever happens first.
//
// Canceling this context releases resources associated with it, so code should
// call cancel as soon as the operations running in this Context complete.
func WithDeadline(parent Context, d time.Time) (Context, CancelFunc) {
	// parent 为空，panic
	if parent == nil {
		panic("cannot create context from nil parent")
	}

	// 检查parent是否已经到了截止时间
	if cur, ok := parent.Deadline(); ok && cur.Before(d) {
		// The current deadline is already sooner than the new one.
		// 目前的截止日期已经比新的截止日期早了，parent已经过了截止时间
		// 执行 parent cancel
		return WithCancel(parent)
	}

	c := &timerCtx{
		cancelCtx: newCancelCtx(parent),
		deadline:  d,
	}

	// 与父节点进行一些关联操作
	propagateCancel(parent, c)

	// 截止时间距离现在  <=0 说明截止时间已经过去 或者 当前就是截止时间
	// <=0 则立即执行取消操作
	dur := time.Until(d)
	if dur <= 0 {
		c.cancel(true, DeadlineExceeded) // deadline has already passed
		return c, func() { c.cancel(false, Canceled) }
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		// 定时器 添加到时间堆
		c.timer = time.AfterFunc(dur, func() {
			c.cancel(true, DeadlineExceeded)
		})
	}

	return c, func() { c.cancel(true, Canceled) }
}

// A timerCtx carries a timer and a deadline. It embeds a cancelCtx to
// implement Done and Err. It implements cancel by stopping its timer then
// delegating to cancelCtx.cancel.
type timerCtx struct {
	cancelCtx
	timer *time.Timer // Under cancelCtx.mu.

	deadline time.Time
}

func (c *timerCtx) Deadline() (deadline time.Time, ok bool) {
	return c.deadline, true
}

func (c *timerCtx) String() string {
	return contextName(c.cancelCtx.Context) + ".WithDeadline(" +
		c.deadline.String() + " [" +
		time.Until(c.deadline).String() + "])"
}

func (c *timerCtx) cancel(removeFromParent bool, err error) {
	c.cancelCtx.cancel(false, err)
	// 从 父节点移除自己
	if removeFromParent {
		// Remove this timerCtx from its parent cancelCtx's children.
		removeChild(c.cancelCtx.Context, c)
	}

	c.mu.Lock()
	if c.timer != nil {
		// ！！！ 重要：不移除则可能会发生短时间内的内存泄漏
		// 从时间堆移除
		c.timer.Stop()
		c.timer = nil
	}

	c.mu.Unlock()
}

// WithTimeout returns WithDeadline(parent, time.Now().Add(timeout)).
//
// Canceling this context releases resources associated with it, so code should
// call cancel as soon as the operations running in this Context complete:
//
// 	func slowOperationWithTimeout(ctx context.Context) (Result, error) {
// 		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
// 		defer cancel()  // releases resources if slowOperation completes before timeout elapses
// 		return slowOperation(ctx)
// 	}
//
// 创建带有超时时间的 ctx
func WithTimeout(parent Context, timeout time.Duration) (Context, CancelFunc) {
	return WithDeadline(parent, time.Now().Add(timeout))
}

// WithValue returns a copy of parent in which the value associated with key is
// val.
//
// Use context Values only for request-scoped data that transits processes and
// APIs, not for passing optional parameters to functions.
//
// The provided key must be comparable and should not be of type
// string or any other built-in type to avoid collisions between
// packages using context. Users of WithValue should define their own
// types for keys. To avoid allocating when assigning to an
// interface{}, context keys often have concrete type
// struct{}. Alternatively, exported context key variables' static
// type should be a pointer or interface.
func WithValue(parent Context, key, val interface{}) Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}

	if key == nil {
		panic("nil key")
	}

	// key 必须是可比较的
	// slice map func 不可比较
	// 包含上面三种类型的结构也不可比较
	if !reflectlite.TypeOf(key).Comparable() {
		panic("key is not comparable")
	}

	return &valueCtx{parent, key, val}
}

// A valueCtx carries a key-value pair. It implements Value for that key and
// delegates all other calls to the embedded Context.
type valueCtx struct {
	Context
	key, val interface{}
}

// stringify tries a bit to stringify v, without using fmt, since we don't
// want context depending on the unicode tables. This is only used by
// *valueCtx.String().
func stringify(v interface{}) string {
	switch s := v.(type) {
	case stringer:
		return s.String()
	case string:
		return s
	}
	return "<not Stringer>"
}

func (c *valueCtx) String() string {
	return contextName(c.Context) + ".WithValue(type " +
		reflectlite.TypeOf(c.key).String() +
		", val " + stringify(c.val) + ")"
}

func (c *valueCtx) Value(key interface{}) interface{} {
	if c.key == key {
		return c.val
	}
	return c.Context.Value(key)
}
