
每当编写的Go代码正确执行之后，总是有一种莫名的感觉——成就感。
但是，作为一个志在远方的码农来说，我们不仅要知其然，也要知其所以然。
在知道Go代码是怎么编写的情况下，还需要了解Go程序的执行过程中都做了些什么，一起来探索吧。


## 运行环境

笔者在整个源码追溯的过程中所依赖的运行环境如下：

```shell
## centos 7.9.2009
[root@localhost go-project]# cat /etc/redhat-release
CentOS Linux release 7.9.2009 (Core)

## linux kernal 3.10.0
[root@localhost go-project]# uname -a
Linux localhost.localdomain 3.10.0-1160.el7.x86_64 #1 SMP Mon Oct 19 16:18:59 UTC 2020 x86_64 x86_64 x86_64 GNU/Linux

## gdb 7.6.1
[root@localhost go-project]# gdb -v
GNU gdb (GDB) Red Hat Enterprise Linux 7.6.1-120.el7
Copyright (C) 2013 Free Software Foundation, Inc.
License GPLv3+: GNU GPL version 3 or later <http://gnu.org/licenses/gpl.html>
This is free software: you are free to change and redistribute it.
There is NO WARRANTY, to the extent permitted by law.  Type "show copying"
and "show warranty" for details.
This GDB was configured as "x86_64-redhat-linux-gnu".
For bug reporting instructions, please see:
<http://www.gnu.org/software/gdb/bugs/>.

## go 1.16.5
## 笔者的go源码位置 /root/go/go1.16.5/
[root@localhost go-project]# go version
go version go1.16.5 linux/amd64
```



## go测试代码

```go
package main

import "fmt"

func main(){
	fmt.Println("hello word")
}
```

很简单的一段代码，打印输出 "hello word"。



## 编译go代码

```shell
[root@localhost demo]# go build -gcflags="-N -l" -o main main.go
```

-gcflags为编译时携带的编译参数，用于告知编译器进行某些处理动作。

```
-N 编译时，禁止优化
-l 编译时，禁止内联
```
通过go build执行之后，得到go的可执行文件main。



## 使用gdb调试

### 加载调试文件

```shell
[root@localhost demo]# gdb main
GNU gdb (GDB) Red Hat Enterprise Linux 7.6.1-120.el7
Copyright (C) 2013 Free Software Foundation, Inc.
License GPLv3+: GNU GPL version 3 or later <http://gnu.org/licenses/gpl.html>
This is free software: you are free to change and redistribute it.
There is NO WARRANTY, to the extent permitted by law.  Type "show copying"
and "show warranty" for details.
This GDB was configured as "x86_64-redhat-linux-gnu".
For bug reporting instructions, please see:
<http://www.gnu.org/software/gdb/bugs/>...
Reading symbols from /root/go-project/demo/main...done.
warning: File "/root/go/go1.16.5/src/runtime/runtime-gdb.py" auto-loading has been declined by your `auto-load safe-path' set to "$debugdir:$datadir/auto-load:/usr/bin/mono-gdb.py".
To enable execution of this file add
	add-auto-load-safe-path /root/go/go1.16.5/src/runtime/runtime-gdb.py
line to your configuration file "/root/.gdbinit".
To completely disable this security protection add
	set auto-load safe-path /
line to your configuration file "/root/.gdbinit".
For more information about this security protection see the
"Auto-loading safe path" section in the GDB manual.  E.g., run from the shell:
	info "(gdb)Auto-loading safe path"

(gdb) source /root/go/go1.16.5/src/runtime/runtime-gdb.py
Loading Go Runtime support.
```

其中需要注意的是，gdb识别出来了go源码中用于gdb调试的文件/root/go/go1.16.5/src/runtime/runtime-gdb.py，使用source命令加载进来。



### 显示go可执行文件调试信息

```she
(gdb) info files
Symbols from "/root/go-project/demo/main".
Local exec file:
	`/root/go-project/demo/main', file type elf64-x86-64.
	Entry point: 0x465740
	0x0000000000401000 - 0x0000000000497773 is .text
	0x0000000000498000 - 0x00000000004dbb44 is .rodata
	0x00000000004dbce0 - 0x00000000004dc40c is .typelink
	0x00000000004dc420 - 0x00000000004dc470 is .itablink
	0x00000000004dc470 - 0x00000000004dc470 is .gosymtab
	0x00000000004dc480 - 0x0000000000534578 is .gopclntab
	0x0000000000535000 - 0x0000000000535020 is .go.buildinfo
	0x0000000000535020 - 0x00000000005432e4 is .noptrdata
	0x0000000000543300 - 0x000000000054aa90 is .data
	0x000000000054aaa0 - 0x00000000005781f0 is .bss
	0x0000000000578200 - 0x000000000057d510 is .noptrbss
	0x0000000000400f9c - 0x0000000000401000 is .note.go.buildid
```

使用gdb的子命令info，来查看目标文件的调试信息。

> (gdb) help
>
> `info files -- Names of targets and files being debugged`

可以看到可执行文件/root/go-project/demo/main的如下信息：

1. file type elf64-x86-64 是64位ELF(Linux可执行文件格式)格式的文件
2. Entry point: 0x465740 程序入口地址是  0x465740
3. 可执行文件的各个段信息以及虚拟内存地址位置信息



### 通过入口地址追溯go启动过程

通过打断点的方式，来找对应的方法调用过程。

```shell
(gdb) b *0x465740
Breakpoint 1 at 0x465740: file /root/go/go1.16.5/src/runtime/rt0_linux_amd64.s, line 8.
```

找到入口位置在 /root/go/go1.16.5/src/runtime/rt0_linux_amd64.s 的第8行

```shell
[root@localhost demo]# vim /root/go/go1.16.5/src/runtime/rt0_linux_amd64.s +8

## /root/go/go1.16.5/src/runtime/rt0_linux_amd64.s 文件
  1 // Copyright 2009 The Go Authors. All rights reserved.
  2 // Use of this source code is governed by a BSD-style
  3 // license that can be found in the LICENSE file.
  4
  5 #include "textflag.h"
  6
  7 TEXT _rt0_amd64_linux(SB),NOSPLIT,$-8
  8         JMP     _rt0_amd64(SB)
  9
```

发现跳到了_rt0_amd64方法中，继续追。

```shell
(gdb) b _rt0_amd64
Breakpoint 2 at 0x4621a0: file /root/go/go1.16.5/src/runtime/asm_amd64.s, line 15.
```

打开_rt0_amd64所在/root/go/go1.16.5/src/runtime/asm_amd64.s文件，找到对应逻辑。

```shell
   [root@localhost demo]# vim /root/go/go1.16.5/src/runtime/asm_amd64.s +15
   
   ## /root/go/go1.16.5/src/runtime/asm_amd64.s 文件
   1 // Copyright 2009 The Go Authors. All rights reserved.
   2 // Use of this source code is governed by a BSD-style
   3 // license that can be found in the LICENSE file.
   4
   5 #include "go_asm.h"
   6 #include "go_tls.h"
   7 #include "funcdata.h"
   8 #include "textflag.h"
   9
  10 // _rt0_amd64 is common startup code for most amd64 systems when using
  11 // internal linking. This is the entry point for the program from the
  12 // kernel for an ordinary -buildmode=exe program. The stack holds the
  13 // number of arguments and the C-style argv.
  14 TEXT _rt0_amd64(SB),NOSPLIT,$-8
  15         MOVQ    0(SP), DI       // argc
  16         LEAQ    8(SP), SI       // argv
  17         JMP     runtime·rt0_go(SB)
  ......
```

由注释可知，_rt0_amd64是大多数amd64系统使用时的通用启动代码。在整个逻辑的第三行(即代码17行)又调用了runtime·rt0_go。

继续对`runtime·rt0_go`打断点，找到对应位置。

> 此处注意：
>
> 1. Go的汇编是基于Plan9的汇编。其中 `runtime·rt0_go` 在gdb调试时变为 `runtime.rt0_go`
>
> 2. 注意那一个点的变化  `·` ->  `.`
>
> 如果你想问为什么go的汇编是基于Plan9的汇编？那么我会告诉我：这帮发明golang的大佬们，当年在贝尔实验室搞出过知名的Unix系统。后来由于某些原因又搞了个plan9系统，可惜plan9系统不怎么知名。大佬或许心有不甘，这不在发明golang语言时，plan9里面的东西终于派上了大用场。

```shell
(gdb) b runtime.rt0_go
Breakpoint 3 at 0x4621c0: file /root/go/go1.16.5/src/runtime/asm_amd64.s, line 91.
```

追溯runtime·rt0_go所在文件以及逻辑。

```shell
[root@localhost demo]# vim /root/go/go1.16.5/src/runtime/asm_amd64.s +91

## /root/go/go1.16.5/src/runtime/asm_amd64.s 文件 runtime·rt0_go代码逻辑
  87 // Defined as ABIInternal since it does not use the stack-based Go ABI (and
  88 // in addition there are no calls to this entry point from Go code).
  89 TEXT runtime·rt0_go<ABIInternal>(SB),NOSPLIT,$0
  90         // copy arguments forward on an even stack
  91         MOVQ    DI, AX          // argc
  92         MOVQ    SI, BX          // argv
  93         SUBQ    $(4*8+7), SP            // 2args 2auto
  94         ANDQ    $~15, SP
  95         MOVQ    AX, 16(SP)
  96         MOVQ    BX, 24(SP)
  97
  98         // create istack out of the given (operating system) stack.
  99         // _cgo_init may update stackguard.
 100         MOVQ    $runtime·g0(SB), DI
 101         LEAQ    (-64*1024+104)(SP), BX
 102         MOVQ    BX, g_stackguard0(DI)
 103         MOVQ    BX, g_stackguard1(DI)
 104         MOVQ    BX, (g_stack+stack_lo)(DI)
 105         MOVQ    SP, (g_stack+stack_hi)(DI)
 106
 107         // find out information about the processor we're on
 108         MOVL    $0, AX
 109         CPUID
 110         MOVL    AX, SI
 111         CMPL    AX, $0
 112         JE      nocpuinfo
 113
 114         // Figure out how to serialize RDTSC.
 115         // On Intel processors LFENCE is enough. AMD requires MFENCE.
 116         // Don't know about the rest, so let's do MFENCE.
 117         CMPL    BX, $0x756E6547  // "Genu"
 118         JNE     notintel
 119         CMPL    DX, $0x49656E69  // "ineI"
 120         JNE     notintel
 121         CMPL    CX, $0x6C65746E  // "ntel"
 122         JNE     notintel
 123         MOVB    $1, runtime·isIntel(SB)
 124         MOVB    $1, runtime·lfenceBeforeRdtsc(SB)
 125 notintel:
 126
 127         // Load EAX=1 cpuid flags
 128         MOVL    $1, AX
 129         CPUID
 130         MOVL    AX, runtime·processorVersionInfo(SB)
 131
 132 nocpuinfo:
 133         // if there is an _cgo_init, call it.
 134         MOVQ    _cgo_init(SB), AX
 135         TESTQ   AX, AX
 136         JZ      needtls
 137         // arg 1: g0, already in DI
 138         MOVQ    $setg_gcc<>(SB), SI // arg 2: setg_gcc
 139 #ifdef GOOS_android
 140         MOVQ    $runtime·tls_g(SB), DX  // arg 3: &tls_g
 141         // arg 4: TLS base, stored in slot 0 (Android's TLS_SLOT_SELF).
 142         // Compensate for tls_g (+16).
 143         MOVQ    -16(TLS), CX
 144 #else
 145         MOVQ    $0, DX  // arg 3, 4: not used when using platform's TLS
 146         MOVQ    $0, CX
 147 #endif
 148 #ifdef GOOS_windows
 149         // Adjust for the Win64 calling convention.
 150         MOVQ    CX, R9 // arg 4
 151         MOVQ    DX, R8 // arg 3
 152         MOVQ    SI, DX // arg 2
 153         MOVQ    DI, CX // arg 1
 154 #endif
 155         CALL    AX
 156
 157         // update stackguard after _cgo_init
 158         MOVQ    $runtime·g0(SB), CX
 159         MOVQ    (g_stack+stack_lo)(CX), AX
 160         ADDQ    $const__StackGuard, AX
 161         MOVQ    AX, g_stackguard0(CX)
 162         MOVQ    AX, g_stackguard1(CX)
 163
 164 #ifndef GOOS_windows
 165         JMP ok
 166 #endif
 167 needtls:
 168 #ifdef GOOS_plan9
 169         // skip TLS setup on Plan 9
 170         JMP ok
 171 #endif
 172 #ifdef GOOS_solaris
 173         // skip TLS setup on Solaris
 174         JMP ok
 175 #endif
 176 #ifdef GOOS_illumos
 177         // skip TLS setup on illumos
 178         JMP ok
 179 #endif
 180 #ifdef GOOS_darwin
 181         // skip TLS setup on Darwin
 182         JMP ok
 183 #endif
 184 #ifdef GOOS_openbsd
 185         // skip TLS setup on OpenBSD
 186         JMP ok
 187 #endif
 188
 189         LEAQ    runtime·m0+m_tls(SB), DI
 190         CALL    runtime·settls(SB)
 191
 192         // store through it, to make sure it works
 193         get_tls(BX)
 194         MOVQ    $0x123, g(BX)
 195         MOVQ    runtime·m0+m_tls(SB), AX
 196         CMPQ    AX, $0x123
 197         JEQ 2(PC)
 198         CALL    runtime·abort(SB)
 199 ok:     // `上面不同的系统最终是跳到了这里`
 200         // set the per-goroutine and per-mach "registers"
 201         get_tls(BX)
 202         LEAQ    runtime·g0(SB), CX 
 203         MOVQ    CX, g(BX)
 204         LEAQ    runtime·m0(SB), AX
 205
 206         // save m->g0 = g0  `!每个m会有一个用于调度的g0，设置m的g0`
 207         MOVQ    CX, m_g0(AX)
 208         // save m0 to g0->m `g0持有m0的地址`
 209         MOVQ    AX, g_m(CX)
 210
 211         CLD                             // convention is D is always left cleared
 212         CALL    runtime·check(SB) 
 213
 214         MOVL    16(SP), AX              // copy argc
 215         MOVL    AX, 0(SP)
 216         MOVQ    24(SP), AX              // copy argv
 217         MOVQ    AX, 8(SP)
 218         CALL    runtime·args(SB)
 219         CALL    runtime·osinit(SB)
 220         CALL    runtime·schedinit(SB)
 221
 222         //create a new goroutine to start program `!创建main goroutine 用于执行runtime.main， 见241行注释`
 223         MOVQ    $runtime·mainPC(SB), AX         // entry
 224         PUSHQ   AX
 225         PUSHQ   $0                      // arg size
 226         CALL    runtime·newproc(SB)
 227         POPQ    AX
 228         POPQ    AX
 229
 230         // start this M  `!让当前线程开始执行 main goroutine`
 231         CALL    runtime·mstart(SB)
 232
 233         CALL    runtime·abort(SB)       // mstart should never return
 234         RET
 235
 236         // Prevent dead-code elimination of debugCallV1, which is
 237         // intended to be called by debuggers.
 238         MOVQ    $runtime·debugCallV1<ABIInternal>(SB), AX
 239         RET
 240
 241 // `mainPC is a function value for runtime.main, to be passed to newproc. `
 242 // The reference to runtime.main is made via ABIInternal, since the
 243 // actual function (not the ABI0 wrapper) is needed by newproc.
 244 DATA    runtime·mainPC+0(SB)/8,$runtime·main<ABIInternal>(SB)
 245 GLOBL   runtime·mainPC(SB),RODATA,$8
```

这段足足有100多行的汇编，其主要作用有以下几点：

1. 根据不同系统初始化寄存器等信息
2. 创建m0、g0
3. 参数处理、系统、调度初始化
4. 调用 runtime.main

> 注意汇编中runtime·rt0_go调用若干方法的所在位置（使用打断点的方式查找）：
>
> runtime·check  -> /root/go/go1.16.5/src/runtime/runtime1.go
>
> runtime.args -> /root/go/go1.16.5/src/runtime/runtime1.go
>
> runtime.osinit -> /root/go/go1.16.5/src/runtime/os_linux.go
>
> runtime.schedinit -> /root/go/go1.16.5/src/runtime/proc.go
>
> runtime.main -> /root/go/go1.16.5/src/runtime/proc.go
>
> runtime.mstart -> /root/go/go1.16.5/src/runtime/proc.go

```go
//------------------
// 几个重要的方法
//------------------

// osinit()确定cpu核心数
301 func osinit() {
302         ncpu = getproccount()
......
323 }


 // ！！！调度器初始化
 592 // The bootstrap sequence is:
 593 //
 594 //      call osinit
 595 //      call schedinit
 596 //      make & queue new G
 597 //      call runtime·mstart
 598 //
 599 // The new G calls runtime·main.
 600 func schedinit() {
 601         lockInit(&sched.lock, lockRankSched)
 602         lockInit(&sched.sysmonlock, lockRankSysmon)
 603         lockInit(&sched.deferlock, lockRankDefer)
 604         lockInit(&sched.sudoglock, lockRankSudog)
 605         lockInit(&deadlock, lockRankDeadlock)
 606         lockInit(&paniclk, lockRankPanic)
 607         lockInit(&allglock, lockRankAllg)
 608         lockInit(&allpLock, lockRankAllp)
 609         lockInit(&reflectOffs.lock, lockRankReflectOffs)
 610         lockInit(&finlock, lockRankFin)
 611         lockInit(&trace.bufLock, lockRankTraceBuf)
 612         lockInit(&trace.stringsLock, lockRankTraceStrings)
 613         lockInit(&trace.lock, lockRankTrace)
 614         lockInit(&cpuprof.lock, lockRankCpuprof)
 615         lockInit(&trace.stackTab.lock, lockRankTraceStackTab)
 616         // Enforce that this lock is always a leaf lock.
 617         // All of this lock's critical sections should be
 618         // extremely short.
 619         lockInit(&memstats.heapStats.noPLock, lockRankLeafRank)
 620
 621         // raceinit must be the first call to race detector.
 622         // In particular, it must be done before mallocinit below calls racemapshadow.
 623         _g_ := getg()
 624         if raceenabled {
 625                 _g_.racectx, raceprocctx0 = raceinit()
 626         }
 627
 628         sched.maxmcount = 10000 // 最大系统线程数限制为 1万
 629
 630         // The world starts stopped.
 631         worldStopped()
 632
 633         moduledataverify()
 634         stackinit() // 栈初始化
 635         mallocinit() // 内存分配器初始化
 636         fastrandinit() // must run before mcommoninit
 637         mcommoninit(_g_.m, -1)
 638         cpuinit()       // must run before alginit
 639         alginit()       // maps must not be used before this call
 640         modulesinit()   // provides activeModules
 641         typelinksinit() // uses maps, activeModules
 642         itabsinit()     // uses activeModules
 643
 644         sigsave(&_g_.m.sigmask)
 645         initSigmask = _g_.m.sigmask
 646
 647         goargs() // 处理命令行参数
 648         goenvs() // 处理环境变量参数
 649         parsedebugvars()
 650         gcinit() // 垃圾回收器初始化
 651
 652         lock(&sched.lock)
 653         sched.lastpoll = uint64(nanotime())
 654         procs := ncpu // ！！！通过cpu core 和 GOMAXPROCS 确定P的数量
 655         if n, ok := atoi32(gogetenv("GOMAXPROCS")); ok && n > 0 {
 656                 procs = n
 657         }
 658         if procresize(procs) != nil { // 调整P数量
 659                 throw("unknown runnable goroutine during bootstrap")
 660         }
 661         unlock(&sched.lock)
 662
 663         // World is effectively started now, as P's can run.
 664         worldStarted()
 665
 666         // For cgocheck > 1, we turn on the write barrier at all times
 667         // and check all pointer writes. We can't do this until after
 668         // procresize because the write barrier needs a P.
 669         if debug.cgocheck > 1 {
 670                 writeBarrier.cgo = true
 671                 writeBarrier.enabled = true
 672                 for _, p := range allp {
 673                         p.wbBuf.reset()
 674                 }
 675         }
 676
 677         if buildVersion == "" {
 678                 // Condition should never trigger. This code just serves
 679                 // to ensure runtime·buildVersion is kept in the resulting binary.
 680                 buildVersion = "unknown"
 681         }
 682         if len(modinfo) == 1 {
 683                 // Condition should never trigger. This code just serves
 684                 // to ensure runtime·modinfo is kept in the resulting binary.
 685                 modinfo = ""
 686         }
 687 }
```

至此，go程序已经基本启动起来，后面就是执行runtime.main的过程。



## 追溯runtime.main

runtime.main是用go语言编写的，到这里已经可以不用看汇编了，是不是很兴奋~

```go
114 // The main goroutine.
 115 func main() {
......		 
 122         // Max stack size is 1 GB on 64-bit, 250 MB on 32-bit.
 123         // Using decimal instead of binary GB and MB because
 124         // they look nicer in the stack overflow failure message.
 125         if sys.PtrSize == 8 { // 设置执行栈的最大限制：64位系统为1G
 126                 maxstacksize = 1000000000
 127         } else {
 128                 maxstacksize = 250000000 // 32位系统为250M	
 129         }
 130
......			 
 139         if GOARCH != "wasm" { // no threads on wasm yet, so no sysmon
 140                 // For runtime_syscall_doAllThreadsSyscall, we
 141                 // register sysmon is not ready for the world to be
 142                 // stopped.
 143                 atomic.Store(&sched.sysmonStarting, 1)
 144                 systemstack(func() { // 启动后台监控线程sysmon，sysmon用处可是非常大的哦
 145                         newm(sysmon, nil, -1) 
 146                 })
 147         }
 148
......
 174         doInit(&runtime_inittask) // 执行runtime包中所有初始化函数init()
......
 184         gcenable() // 启动垃圾回收器进行后台操作
......
 208         doInit(&main_inittask) // 执行所有用户包中初始化函数init()
......			 
 224         fn := main_main // 执行用户逻辑入口 main.main，就是我们写的那个main()函数
 225         fn()
......
 252 }
```

至此，go程序已经完全启动起来，并开始执行我们的代码了。

# 总结

本文基于Linux环境,一步步的追溯go程序启动的大致过程：_rt0_amd64_linux -> _rt0_amd64 -> runtime·rt0_go -> runtime.main -> main.main。

你可能会问：为什么需要学习go程序的启动过程？

笔者想说的是：在日常开发或面试过程中，你可能会听到go的GPM模型、P的总数量由runtime.GOMAXPROCS()控制、init()的初始化过程、go线程最大数量限制、go栈最大限制等等问题时，不止是知道它的存在，而是需要知道它为什么存在以及存在哪里。

所谓：知己知彼百战百胜。这样，在开发过程中才能更得心应手。


## 参考

> 1. 《Go语言学习笔记》 雨痕/著
> 2. 《初识Go汇编》/《Golang 汇编入门知识总结》

