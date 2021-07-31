# Name resolving

This examples shows how `ClientConn` can pick different name resolvers.
> 这是一个展示 ClientConn 如何选择不同 名称解析器 的例子

## What is a name resolver

A name resolver can be seen as a `map[service-name][]backend-ip`. It takes a
service name, and returns a list of IPs of the backends. A common used name
resolver is DNS.
> 一个名称解析器可被看做是`map[service-name][]backend-ip`
> 通过服务名称来获取后端可用的ip地址列表，通常使用的名称解析器是 DNS

In this example, a resolver is created to resolve `resolver.example.grpc.io` to
`localhost:50051`.
> 在这个例子中 解析器用来解析 `resolver.example.grpc.io` 到 `localhost:50051`

## Try it

```
go run server/main.go
```

```
go run client/main.go
```

## Explanation

The echo server is serving on ":50051". Two clients are created, one is dialing
to `passthrough:///localhost:50051`, while the other is dialing to
`example:///resolver.example.grpc.io`. Both of them can connect the server.
> 这里服务端在 ":50051" 提供服务
> 两个客户端被创建，其中一个客户端使用 `passthrough:///localhost:50051` 来连接
> 另外一个 使用 `example:///resolver.example.grpc.io` 来连接
> 他们都能连接到服务端

Name resolver is picked based on the `scheme` in the target string. See
https://github.com/grpc/grpc/blob/master/doc/naming.md for the target syntax.
> 名称解析器选择的方式是基于 目标地址的 `scheme`
> 可通过 https://github.com/grpc/grpc/blob/master/doc/naming.md 来学习对应语法规则

The first client picks the `passthrough` resolver, which takes the input, and
use it as the backend addresses.
> 第一个客户端通过 `passthrough` 来解析，使用它来作为服务端地址

The second is connecting to service name `resolver.example.grpc.io`. Without a
proper name resolver, this would fail. In the example it picks the `example`
resolver that we installed. The `example` resolver can handle
`resolver.example.grpc.io` correctly by returning the backend address. So even
though the backend IP is not set when ClientConn is created, the connection will
be created to the correct backend.
> 第二个客户端通过 服务名 `resolver.example.grpc.io` 来连接
> 如果没有合适的服务名解析器，将会失败。
> 在这个例子里面我们选择使用 `example` 来替代。
> “example”解析器可以正确的处理`resolver.example.grpc.io`返回的后端地址。
> 所以即使尽管在创建ClientConn时没有设置后端IP，但是连接将被设置创建到正确的后端。