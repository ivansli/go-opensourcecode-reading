# gRPC-Go

原grpc-go官方 [README.md](./README_en.md)

# 源码追踪

源码追踪主要从 example 目录中的各个使用例子开始

- example/helloworld 是一个简单的例子<br/>
> 主要通过该例子追溯源码

```
├── greeter_client
│   └── main.go
├── greeter_server
│   └── main.go
└── helloworld
    ├── helloworld.pb.go
    ├── helloworld.proto
    └── helloworld_grpc.pb.go
```

- example/features 包含了gRpc-go的各种特性使用（重要）

```
├── authentication # 校验认证
├── cancellation # cancel上下文
├── compression  # 数据传输压缩相关
├── deadline # 超时控制
├── debugging
├── encryption 
├── errors 
├── health
├── interceptor
├── keepalive # 长连接
├── load_balancing # 负载均衡
├── metadata  # metadata相关
├── multiplex
├── name_resolving
├── name_resolving_etcd # 使用etcd服务的名称解析
├── proto
├── reflection
├── retry
├── wait_for_ready
└── xds
```

## 客户端逻辑流程

```go
// conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
// clientconn.go
// 创建连接conn
func Dial(target string, opts ...DialOption) (*ClientConn, error)

// c := pb.NewGreeterClient(conn)
// 把连接封装到proto生成的go中client对象中
func NewGreeterClient(cc grpc.ClientConnInterface) GreeterClient

//r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
//
// proto生成的go中
func (c *greeterClient) SayHello(ctx context.Context, in *HelloRequest, opts ...grpc.CallOption) (*HelloReply, error)
//err := c.cc.Invoke(ctx, "/helloworld.Greeter/SayHello", in, out, opts...)

// call.go文件
func (cc *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...CallOption) error
//invoke(ctx, method, args, reply, cc, opts...)

func invoke(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error
// cs, err := newClientStream(ctx, unaryStreamDesc, cc, method, opts...)

// stream.go文件
func newClientStream(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (_ ClientStream, err error)
// newClientStreamWithParams(ctx, desc, cc, method, mc, onCommit, done, opts...)

// stream.go文件
func newClientStreamWithParams(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, mc serviceconfig.MethodConfig, onCommit, doneFunc func(), opts ...CallOption) (_ iresolver.ClientStream, err error)
// op := func(a *csAttempt) error { return a.newStream() }
// cs.withRetry(op, func() { cs.bufferForRetryLocked(0, op) })

// stream.go文件
// 为rpc创建stream
func (a *csAttempt) newStream() error
// s, err := a.t.NewStream(cs.ctx, cs.callHdr)

// grpc-go/internal/transport/http2_client.go
// a.t.NewStream
// TODO 追源码
// 很重要
// 创建stream以及消息头信息等
func (t *http2Client) NewStream(ctx context.Context, callHdr *CallHdr) (_ *Stream, err error)

//
// SendMsg RecvMsg 都是 type ServerStream interface 接口中定义方法
//

// call.go文件
// func invoke(ctx context.Context, method string, req, reply interface{}, cc *ClientConn, opts ...CallOption) error
// cs.SendMsg(req)
func (cs *clientStream) SendMsg(m interface{}) (err error)
func (a *csAttempt) sendMsg(m interface{}, hdr, payld, data []byte) error
func (as *addrConnStream) SendMsg(m interface{}) (err error)

// call.go文件
// cs.RecvMsg(reply)
func (ss *serverStream) RecvMsg(m interface{}) (err error)
func (cs *clientStream) RecvMsg(m interface{}) error
func (a *csAttempt) recvMsg(m interface{}, payInfo *payloadInfo) (err error)
func (as *addrConnStream) RecvMsg(m interface{}) (err error)

```

## 服务端逻辑流程

server.go

```go
// 获取grpc sever对象
// s := grpc.NewServer()
func NewServer(opt ...ServerOption) *Server

// 注册服务到 s对象中
// pb.RegisterGreeterServer(s, &server{})
// 
// RegisterGreeterServer 在proto生成的go文件中
func RegisterGreeterServer(s grpc.ServiceRegistrar, srv GreeterServer)
// RegisterGreeterServer 调用 server.go文件 RegisterService方法
func (s *Server) RegisterService(sd *ServiceDesc, ss interface{})

// 对外提供服务
// s.Serve(lis)
func (s *Server) Serve(lis net.Listener) error
//s.handleRawConn(lis.Addr().String(), rawConn)
func (s *Server) handleRawConn(lisAddr string, rawConn net.Conn)
// s.serveStreams(st)
func (s *Server) serveStreams(st transport.ServerTransport)
// s.handleStream(st, stream, s.traceInfo(st, stream))
func (s *Server) handleStream(t transport.ServerTransport, stream *transport.Stream, trInfo *traceInfo)
// s.processUnaryRPC(t, stream, srv, md, trInfo)
// s.processStreamingRPC(t, stream, srv, sd, trInfo)
func (s *Server) processUnaryRPC(t transport.ServerTransport, stream *transport.Stream, info *serviceInfo, md *MethodDesc, trInfo *traceInfo) (err error)
func (s *Server) processStreamingRPC(t transport.ServerTransport, stream *transport.Stream, info *serviceInfo, sd *StreamDesc, trInfo *traceInfo) (err error)
```

# 客户端连接创建过程

```go
// 以 grpc.Dial 为例 
conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
```

调用过程

```go
// clientconn.go
func Dial(target string, opts ...DialOption) (*ClientConn, error)
// DialContext(context.Background(), target, opts...)

// clientconn.go
func DialContext(ctx context.Context, target string, opts ...DialOption) (conn *ClientConn, err error)
// rWrapper, err := newCCResolverWrapper(cc, resolverBuilder)

// resolver_conn_wrapper.go
func newCCResolverWrapper(cc *ClientConn, rb resolver.Builder) (*ccResolverWrapper, error)
// ccr.resolver, err = rb.Build(cc.parsedTarget, ccr, rbo)

// 以 passthrough internal/resolver/passthrough/passthrough.go 为例
func (*passthroughBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error)
// r.start()

// internal/resolver/passthrough/passthrough.go
func (r *passthroughResolver) start()
// r.cc.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: r.target.Endpoint}}})

// resolver_conn_wrapper.go
func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error
// ccr.cc.updateResolverState(ccr.curState, nil)

// clientconn.go
func (cc *ClientConn) updateResolverState(s resolver.State, err error) error
// bw.updateClientConnState(&balancer.ClientConnState{ResolverState: s, BalancerConfig: balCfg})

// resolver_conn_wrapper.go
func (ccb *ccBalancerWrapper) updateClientConnState(ccs *balancer.ClientConnState) error
// ccb.balancer.UpdateClientConnState(*ccs)

// pickfirst.go
func (b *pickfirstBalancer) UpdateClientConnState(cs balancer.ClientConnState) error
// b.sc.Connect()

// balancer_conn_wrappers.go
func (acbw *acBalancerWrapper) Connect()
// acbw.ac.connect()

// clientconn.go
func (ac *addrConn) connect() error
// go ac.resetTransport()    看到启动了新的 goroutine 建立连接

// clientconn.go
func (ac *addrConn) resetTransport()
// newTr, addr, reconnect, err := ac.tryAllAddrs(addrs, connectDeadline)
// 
// 备注：ac.startHealthCheck(hctx) 启动新的goroutine进行健康检查

// clientconn.go
// 核心：尝试从多个地址中建立连接
func (ac *addrConn) tryAllAddrs(addrs []resolver.Address, connectDeadline time.Time) (transport.ClientTransport, resolver.Address, *grpcsync.Event, error)
// newTr, reconnect, err := ac.createTransport(addr, copts, connectDeadline)

// clientconn.go
func (ac *addrConn) createTransport(addr resolver.Address, copts transport.ConnectOptions, connectDeadline time.Time) (transport.ClientTransport, *grpcsync.Event, error)
// newTr, err := transport.NewClientTransport(connectCtx, ac.cc.ctx, addr, copts, onPrefaceReceipt, onGoAway, onClose)

// transport.go
func NewClientTransport(connectCtx, ctx context.Context, addr resolver.Address, opts ConnectOptions, onPrefaceReceipt func (), onGoAway func (GoAwayReason), onClose func ()) (ClientTransport, error)
// newHTTP2Client(connectCtx, ctx, addr, opts, onPrefaceReceipt, onGoAway, onClose)

// http2_client.go
func newHTTP2Client(connectCtx, ctx context.Context, addr resolver.Address, opts ConnectOptions, onPrefaceReceipt func (), onGoAway func (GoAwayReason), onClose func ()) (_ *http2Client, err error)
// conn, err := dial(connectCtx, opts.Dialer, addr, opts.UseProxy, opts.UserAgent)

// http2_client.go
func dial(ctx context.Context, fn func(context.Context, string) (net.Conn, error), addr resolver.Address, useProxy bool, grpcUA string) (net.Conn, error)
// (&net.Dialer{}).DialContext(ctx, networkType, address)

// 此时已经进入到go的内核源码net包中
// src/go/net/dial.go
func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error)
// 后面的调用已经不属于grpc框架了...

```

从上面可以看出grpc客户端建立一个conn则调用了几十个方法，包含了众多逻辑。

# 参考以及扩展阅读

https://segmentfault.com/a/1190000039742489   grpc源码学习笔记
