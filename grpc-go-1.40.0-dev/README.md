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

# 关于 gRpc 源码学习笔记

1. [客户端与服务端调用过程追溯](./docs/客户端与服务端调用过程追溯.md)
2. [名称解析与负载均衡](./docs/名称解析与负载均衡.md)
3. [值得学习的结构体实现](./docs/值得学习的结构体.md)




