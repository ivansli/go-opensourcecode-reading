# 负载均衡器的选择器 picker 更新流程

## 更新 picker

func DialContext(ctx context.Context, target string, opts ...DialOption) (conn *ClientConn, err error)

func newCCResolverWrapper(cc *ClientConn, rb resolver.Builder) (*ccResolverWrapper, error)
> ccr.resolver, err = rb.Build(cc.parsedTarget, ccr, rbo)

func (b *Builder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error)
> r.cc.UpdateState(resolver.State{Addresses: addrs})

func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error 
> ccr.cc.updateResolverState(ccr.curState, nil)

func (cc *ClientConn) updateResolverState(s resolver.State, err error) error
>> cc.applyServiceConfigAndBalancer(sc, configSelector, s.Addresses) <br/>
>> --- func (cc *ClientConn) applyServiceConfigAndBalancer(......) <br/>
>> ------ newCCBalancerWrapper(cc, cc.dopts.balancerBuilder, cc.balancerBuildOpts) <br/>
>> --------- go ccb.watcher() <br/>
>> ----------- 注意 此时的 ccb.watcher()，监听 ccb.updateCh, 接收到 scStateUpdate 会更新 picker  <br/>
```go
// watcher balancer 函数顺序，所以平衡器可以实现无锁。
func (ccb *ccBalancerWrapper) watcher() {
	for {
		select {
		// 监听 负载均衡的更新通道 updateCh
		case t := <-ccb.updateCh.Get():
			// 载入 backlog 中违背消费的数据
			......
			
			switch u := t.(type) {
			case *scStateUpdate: // 更新状态
			    ......
			    
				// 更新连接信息
				// 同时更新 clientconn 对象中的 负载均衡器的选择器 picker
				ccb.balancer.UpdateSubConnState(u.sc, balancer.SubConnState{ConnectivityState: u.state, ConnectionError: u.err})
				......
		}
		......
	}
}
```
>> -- func (b *baseBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) <br/>
>> ---- b.cc.UpdateState(balancer.State{ConnectivityState: b.state, Picker: b.picker}) <br/>
>> ------ func (ccb *ccBalancerWrapper) UpdateState(s balancer.State) <br/>
>> -------- ccb.cc.blockingpicker.updatePicker(s.Picker)  <br/>
>> ---------- func (pw *pickerWrapper) updatePicker(p balancer.Picker) <br/>
>> ----------- pw.picker = p  // 至此 更新了 picker  <br/>

> bw.updateClientConnState(&balancer.ClientConnState{ResolverState: s, BalancerConfig: balCfg})


func (ccb *ccBalancerWrapper) updateClientConnState(ccs *balancer.ClientConnState) error
> ccb.balancer.UpdateClientConnState(*ccs)

// 这里用的 roundrobin, balancer/base/balancer.go
func (b *baseBalancer) UpdateClientConnState(s balancer.ClientConnState) error
> sc.Connect()

func (ac *addrConn) connect() error
> go ac.resetTransport()

---

// 启用新的 goroutine 执行 ac.resetTransport()
func (ac *addrConn) resetTransport()
> ac.startHealthCheck(hctx)

func (ac *addrConn) startHealthCheck(ctx context.Context) 
> ac.updateConnectivityState(connectivity.Ready, nil)

func (ac *addrConn) updateConnectivityState(s connectivity.State, lastErr error) 
> ac.cc.handleSubConnStateChange(ac.acbw, s, lastErr)

func (cc *ClientConn) handleSubConnStateChange(sc balancer.SubConn, s connectivity.State, err error)
> cc.balancerWrapper.handleSubConnStateChange(sc, s, err)

func (ccb *ccBalancerWrapper) handleSubConnStateChange(sc balancer.SubConn, s connectivity.State, err error) 
```go
ccb.updateCh.Put(&scStateUpdate{
		sc:    sc,
		state: s,
		err:   err,
	})
```

---

## 使用 picker 选择连接

在 invoke 方法中 会使用 picker 来选择 建立好的连接

func (cc *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...CallOption) error {
> invoke(ctx, method, args, reply, cc, opts...)

func invoke(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error 
> newClientStream(ctx, unaryStreamDesc, cc, method, opts...)

func newClientStream(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (_ ClientStream, err error) 
> newClientStreamWithParams(ctx, desc, cc, method, mc, onCommit, done, opts...)

func newClientStreamWithParams(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, mc serviceconfig.MethodConfig, onCommit, doneFunc func(), opts ...CallOption) (_ iresolver.ClientStream, err error) 
> cs.newAttemptLocked(sh, trInfo)

func (cs *clientStream) newAttemptLocked(sh stats.Handler, trInfo *traceInfo) (retErr error) 
> cs.cc.getTransport(ctx, cs.callInfo.failFast, cs.callHdr.Method)

func (cc *ClientConn) getTransport(ctx context.Context, failfast bool, method string) (transport.ClientTransport, func(balancer.DoneInfo), error)
> cc.blockingpicker.pick(ctx, failfast, balancer.PickInfo{ Ctx:ctx, FullMethodName: method})

func (pw *pickerWrapper) pick(ctx context.Context, failfast bool, info balancer.PickInfo) (transport.ClientTransport, func(balancer.DoneInfo), error) 
> p := pw.picker <br/>
> pickResult, err := p.Pick(info) <br/>

使用 roundrobin，则 pick 在 balancer/roundrobin/roundrobin.go
```go
func (p *rrPicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	p.mu.Lock()

	// 选中的
	sc := p.subConns[p.next]
	// 由于是轮询，所以next+1
	p.next = (p.next + 1) % len(p.subConns)

	p.mu.Unlock()

	return balancer.PickResult{SubConn: sc}, nil
}
```

> pickResult, err := p.Pick(info) <br/>
> acw, ok := pickResult.SubConn.(*acBalancerWrapper) <br/>
> acw.getAddrConn().getReadyTransport() // 此时就获取到就绪的 Transport <br/> 





