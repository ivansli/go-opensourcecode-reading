

在Go中有一个特殊的线程，它不与其他任何P进行绑定。在一个死循环之中不停的执行一系列的监控操作，通过这些监控操作来更好的服务于整个Go进程，它就是——sysmon监控线程。

你可能会好奇它的作用，这里简单总结一下：

- 释放闲置超过5分钟的span物理内存
- 超过2分钟没有垃圾回收，强制启动
- 将长时间没有处理的netpoll结果添加到任务队列
- 向长时间执行的G任务发起抢占调度
- 收回因syscall而长时间阻塞的P

因此可以看出，sysmon线程就像监工一样，监控着整个进程的状态。你会不会跟我一样好奇这个线程是怎么启动起来的，一起来追溯吧。



## 准备工作

- Go源码版本基于：1.16.5
- IDE：goland
- 操作系统：Centos
- 知识储备：了解Go的启动过程，见笔者的文章《Go程序启动过程的一次追溯》

> Go的启动过程大概分类三个阶段
>
> 1. Go程序的引导过程
> 2. runtime的启动以及初始化过程（runtime.main）
> 3. 执行用户代码（main.main）



## sysmon启动过程追溯

由Go的启动过程大概可以猜出来，sysmon的启动过程则属于runtime的启动以及初始化过程。所以，我们从runtime.main开始一步步的追溯代码，来寻找sysmon的启动步骤。

runtime/proc.go

```go
func main() {
  	...
  	if GOARCH != "wasm" { // no threads on wasm yet, so no sysmon
      // For runtime_syscall_doAllThreadsSyscall, we
      // register sysmon is not ready for the world to be
      // stopped.
      
      // !!! 找到了 启动sysmon的代码
      // 在系统栈内生成一个新的M来启动sysmon
      atomic.Store(&sched.sysmonStarting, 1)
        systemstack(func() {
          newm(sysmon, nil, -1)
      })
	}
  ...
}

// 创建一个新的系统线程
// Create a new m. It will start off with a call to fn, or else the scheduler.
// fn needs to be static and not a heap allocated closure.
// May run with m.p==nil, so write barriers are not allowed.
//
// id is optional pre-allocated m ID. Omit by passing -1.
//go:nowritebarrierrec
func newm(fn func(), _p_ *p, id int64) {
	// 获取GPM中M结构体，并进行部分字段的初始化
  // allocm方法非常重要！！！
  // 该方法不仅获取并初始化M的结构体，还在M里面设置了系统线程将要执行的具体方法fn，这里是sysmon
	mp := allocm(_p_, fn, id)
	...
  
  // M在Go中属于用户态代码中的一个结构体，跟系统线程是一对一的关系
  // 每个系统线程怎么执行代码，从哪里开始执行，则是由M的结构体中参数来指明
	// 创建GPM中结构体M结构体之后，开始创建对应的底层系统线程
	newm1(mp)
}

// 给M分配一个系统线程
// Allocate a new m unassociated with any thread.
// Can use p for allocation context if needed.
// fn is recorded as the new m's m.mstartfn.
// id is optional pre-allocated m ID. Omit by passing -1.
//
// This function is allowed to have write barriers even if the caller
// isn't because it borrows _p_.
//
//go:yeswritebarrierrec
func allocm(_p_ *p, fn func(), id int64) *m {
	...
	// 创建新的M，并且进行一些初始化操作
	mp := new(m)
	// M 的执行方法, 在runtime.mstart()方法中最终调用fn
	mp.mstartfn = fn
	...
}

// 楷书创建系统线程的逻辑
func newm1(mp *m) {
  ...
	// ！！！创建系统线程！！！
	newosproc(mp)
	...
}
```

runtime/os_linux.go

```go
// 通过clone创建系统线程
// May run with m.p==nil, so write barriers are not allowed.
//go:nowritebarrier
func newosproc(mp *m) {
	...
	// Disable signals during clone, so that the new thread starts
	// with signals disabled. It will enable them in minit.
  //
  // 注意：
  // 第5个参数 mstart 是在 runtime.mstart
	ret := clone(cloneFlags, stk, unsafe.Pointer(mp), unsafe.Pointer(mp.g0), unsafe.Pointer(funcPC(mstart)))
	...
}

//go:noescape
//clone没有具体方法体，具体实现使用汇编编写
func clone(flags int32, stk, mp, gp, fn unsafe.Pointer) int32
```

> clone()函数在linux系统中，用来创建轻量级进程

runtime/sys_linux_arm64.s

```assembly
// 注意 这里的void (*fn)(void) 就是 runtime.mstart 方法的地址入口
//
// int64 clone(int32 flags, void *stk, M *mp, G *gp, void (*fn)(void));
TEXT runtime·clone(SB),NOSPLIT|NOFRAME,$0
	...
	// Copy mp, gp, fn off parent stack for use by child.
	MOVD	mp+16(FP), R10
	MOVD	gp+24(FP), R11
	MOVD	fn+32(FP), R12 // 注意R12寄存器存储的是fn的地址哦
	...
	
	// 判断是父进程，则直接返回
	// 子进程则跳到 child
	// In parent, return.
	CMP	ZR, R0
	BEQ	child
	MOVW	R0, ret+40(FP)
	RET
	
child:
	// In child, on new stack.
	MOVD	-32(RSP), R10
	MOVD	$1234, R0
	CMP	R0, R10
	BEQ	good
	...

good:
	...
	CMP	$0, R10
	BEQ	nog
	CMP	$0, R11
	BEQ	nog
	...
nog:
	// Call fn,  调用 fn，即 runtime.mstart
	MOVD	R12, R0 // R12中存放的是fn的地址
	BL	(R0)  // BL是一个跳转指令，跳转到fn

...
```

runtime.proc.go

```go
// mstart是一个M的执行入口
// 
// mstart is the entry-point for new Ms.
//
// This must not split the stack because we may not even have stack
// bounds set up yet.
//
// May run during STW (because it doesn't have a P yet), so write
// barriers are not allowed.
//
//go:nosplit
//go:nowritebarrierrec
func mstart() {
	...
	mstart1()
	...
}

// 开始执行M的具体方法
func mstart1() {
	_g_ := getg()
  
	...
	// M中mstartfn指向 runtime.sysmon， 即 fn = runtime.sysmon
	if fn := _g_.m.mstartfn; fn != nil {
    // 即：执行 runtime.sysmon
    // sysmon方法是一个死循环，所以说执行sysmon的线程会一直在这里
		fn()
	}
	...
}
```

最终执行的sysmon方法

```go
// Always runs without a P, so write barriers are not allowed.
//
//go:nowritebarrierrec
func sysmon() {
	...
	for {
    ...
		// 获取超过10ms的netpoll结果
		//
		// poll network if not polled for more than 10ms
		lastpoll := int64(atomic.Load64(&sched.lastpoll))
		if netpollinited() && lastpoll != 0 && lastpoll+10*1000*1000 < now {
			atomic.Cas64(&sched.lastpoll, uint64(lastpoll), uint64(now))
			list := netpoll(0) // non-blocking - returns list of goroutines
			if !list.empty() {
				// Need to decrement number of idle locked M's
				// (pretending that one more is running) before injectglist.
				// Otherwise it can lead to the following situation:
				// injectglist grabs all P's but before it starts M's to run the P's,
				// another M returns from syscall, finishes running its G,
				// observes that there is no work to do and no other running M's
				// and reports deadlock.
				incidlelocked(-1)
				injectglist(&list)
				incidlelocked(1)
			}
		}

		...

		// 抢夺syscall长时间阻塞的P，向长时间阻塞的P发起抢占调度
		//
		// retake P's blocked in syscalls
		// and preempt long running G's
		if retake(now) != 0 {
			idle = 0
		} else {
			idle++
		}
    
    // 检查是否需要强制执行垃圾回收
		// check if we need to force a GC
		if t := (gcTrigger{kind: gcTriggerTime, now: now}); t.test() && atomic.Load(&forcegc.idle) != 0 {
			lock(&forcegc.lock)
			forcegc.idle = 0
			var list gList
			list.push(forcegc.g)
			injectglist(&list)
			unlock(&forcegc.lock)
		}
		...
	}
  ...
}
```

# 总结

以上可知，sysmon线程的创建过程经过若干阶段：

1. 创建M对应结构体，对该结构初始化并绑定系统线程未来将要执行的方法sysmon
2. 给M创建对应的底层系统线程（不同的操作系统生成系统线程方式不同）
3. 引导系统线程从mstart方法开始执行sysmon逻辑（sysmon方法是死循环）

sysmon线程启动之后就进入监控整个Go进程的逻辑中，至于sysmon都做了些什么，有机会再一起探讨。


