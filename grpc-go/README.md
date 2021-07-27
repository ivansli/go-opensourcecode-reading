# gRPC-Go

原grpc-go官方 [README.md](./README_en.md)


# 源码追踪
源码追踪主要从 example 目录中的各个使用例子开始

- example/helloworld 是一个简单的例子<br/>
> 通过该例子追溯交互逻辑
>

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


# 参考以及扩展阅读

https://segmentfault.com/a/1190000039742489   grpc源码学习笔记
