/*
 *
 * Copyright 2017 gRPC authors.
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

package base

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

var logger = grpclog.Component("balancer")

type baseBuilder struct {
	name          string
	pickerBuilder PickerBuilder
	config        Config
}

// 负载均衡器的 构造器 创建一个 负载均衡器
func (bb *baseBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	// cc 为负载均衡包装器，被包含在了 负载均衡器 中
	// cc 为 balancer_conn_wrappers.go 的 ccBalancerWrapper 结构体
	bal := &baseBalancer{
		cc:            cc, // cc 为 ccBalancerWrapper
		pickerBuilder: bb.pickerBuilder,

		subConns: make(map[resolver.Address]subConnInfo),
		scStates: make(map[balancer.SubConn]connectivity.State),
		csEvltr:  &balancer.ConnectivityStateEvaluator{},
		config:   bb.config,
	}

	// Initialize picker to a picker that always returns
	// ErrNoSubConnAvailable, because when state of a SubConn changes, we
	// may call UpdateState with this picker.
	// 初始化一个永远返回 ErrNoSubConnAvailable 错误的 picker
	// 因为 当 SubConn 的状态变化时，我们应该调用 UpdateState 取更新 picker
	bal.picker = NewErrPicker(balancer.ErrNoSubConnAvailable)

	return bal
}

func (bb *baseBuilder) Name() string {
	return bb.name
}

type subConnInfo struct {
	subConn balancer.SubConn
	attrs   *attributes.Attributes
}

type baseBalancer struct {
	cc balancer.ClientConn

	// Picker 的构建器
	// type PickerBuilder interface {
	//    Build(info PickerBuildInfo) balancer.Picker
	// }
	pickerBuilder PickerBuilder

	// 记录 [当前 负载均衡器下] 多个 subcons 的 就绪连接、 连接中 的个数
	csEvltr *balancer.ConnectivityStateEvaluator
	state   connectivity.State

	// 名称解析器给的所有的可用地址 的 连接信息与状态
	subConns map[resolver.Address]subConnInfo // `attributes` is stripped from the keys of this map (the addresses)
	scStates map[balancer.SubConn]connectivity.State

	// type Picker interface {
	//    Pick(info PickInfo) (PickResult, error)
	// }
	// 负载均衡器在选择某一个连接时 调用 picker 的 Pick 方法
	picker balancer.Picker
	config Config

	resolverErr error // the last error reported by the resolver; cleared on successful resolution
	connErr     error // the last connection error; cleared upon leaving TransientFailure
}

func (b *baseBalancer) ResolverError(err error) {
	b.resolverErr = err
	if len(b.subConns) == 0 {
		b.state = connectivity.TransientFailure
	}

	if b.state != connectivity.TransientFailure {
		// The picker will not change since the balancer does not currently
		// report an error.
		return
	}

	// 生成负载均衡器的选择器
	b.regeneratePicker()

	// ！！！更新
	b.cc.UpdateState(balancer.State{
		ConnectivityState: b.state,
		Picker:            b.picker,
	})
}

// TODO （read code）
//  使用 roundrobin 负载均衡器时 在这里 不仅建立连接，还针对不同地址建立的连接进行状态更新
//  还会有其他负载均衡算法
//
// 最终 创建的连接 存储在 b.subConns 对象中
// 在真正请求远端时，由负载均衡器来 选择(pick) 一个连接
func (b *baseBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	// TODO: handle s.ResolverState.ServiceConfig?
	if logger.V(2) {
		logger.Info("base.baseBalancer: got new ClientConn state: ", s)
	}

	// Successful resolution; clear resolver error and ensure we return nil.
	// 清除解析器错误并确保返回 nil
	b.resolverErr = nil

	// addrsSet is the set converted from addrs, it's used for quick lookup of an address.
	//
	// addrsSet 主要用来记录 s.ResolverState.Addresses 中出现的地址
	// 	  出现在 b.subConns 但是没出现在 addrsSet 中的地址连接将被移除
	addrsSet := make(map[resolver.Address]struct{})

	// TODO （追代码）
	// 对每一个地址建立一个连接
	// 等invoke调用的时候，选取一个地址，然后获取它对应的连接直接使用
	for _, a := range s.ResolverState.Addresses {
		// Strip attributes from addresses before using them as map keys. So
		// that when two addresses only differ in attributes pointers (but with
		// the same attribute content), they are considered the same address.
		//
		// Note that this doesn't handle the case where the attribute content is
		// different. So if users want to set different attributes to create
		// duplicate connections to the same backend, it doesn't work. This is
		// fine for now, because duplicate is done by setting Metadata today.
		//
		// 在使用地址作为映射键之前，先从地址中剥离属性
		// 因此，当两个地址只是属性指针不同(但属性内容相同)时，它们被认为是相同的地址。
		//
		// 注意，这并不处理属性内容不同的情况。
		// 因此，如果用户想要设置不同的属性来创建到同一后端的重复连接，它是不起作用的。
		// 现在这样做很好，因为复制是通过今天设置Metadata完成的。
		//
		// TODO: read attributes to handle duplicate connections.
		//
		// Attributes 属性置空的 address 信息
		//
		// 注意：
		// 	最开始对应地址的连接信息不存在时，Attributes 设置为空
		//  当后面再次执行本方法来更新conn状态时，如果地址存在对应连接。则只更新 Attributes 信息
		aNoAttrs := a
		aNoAttrs.Attributes = nil
		addrsSet[aNoAttrs] = struct{}{}

		// aNoAttrs 对应的地址没建立连接，则需要使用conn建立连接
		// b.subConns[aNoAttrs]
		//    表示从负载均衡器的 subConns 的 map中查看是否已经存在 该地址的连接信息
		//
		if scInfo, ok := b.subConns[aNoAttrs]; !ok {
			// 不存在，则创建
			// a is a new address (not existing in b.subConns).
			// 是一个新的地址（不在b.subConns中存在）
			//
			// When creating SubConn, the original address with attributes is
			// passed through. So that connection configurations in attributes
			// (like creds) will be used.
			//
			// 在创建SubConn时，将传递带有属性的原始地址
			// 因此，属性中的连接配置(如creds)将被使用

			// 对每一个没建立连接信息的 地址创建 SubConn 对象，保存连接信息
			//
			// b.cc 为  balancer.ClientConn 接口类型
			//
			// b 为 负载均衡器，b.cc 为 负载均衡器的包装器
			// 		即：balancer_conn_wrappers.go 中 ccBalancerWrapper 结构体

			// sc 为 balancer_conn_wrappers.go 中 addrConn 对象的 包装对象 acBalancerWrapper
			// acBalancerWrapper
			sc, err := b.cc.NewSubConn([]resolver.Address{a}, balancer.NewSubConnOptions{HealthCheckEnabled: b.config.HealthCheck})
			if err != nil {
				logger.Warningf("base.baseBalancer: failed to create new SubConn: %v", err)
				continue
			}

			b.subConns[aNoAttrs] = subConnInfo{subConn: sc, attrs: a.Attributes}
			// 设置状态 为 idle
			b.scStates[sc] = connectivity.Idle

			///////////////////////////////////////////////
			// !!!核心
			// 当前地址还没创建连接 conn，创建连接
			//
			// balancer_conn_wrappers.go 文件中
			///////////////////////////////////////////////
			// 调用 acBalancerWrapper 的 Connect 方法， 即 最终是 addrConn 的 conn 方法

			// addrConn 对应的连接状态 一直由一个新的协程来维持
			// acBalancerWrapper.Connect
			sc.Connect()
		} else {
			// aNoAttrs上建立连接，则进行更新

			// Always update the subconn's address in case the attributes
			// changed.
			//
			// The SubConn does a reflect.DeepEqual of the new and old
			// addresses. So this is a noop if the current address is the same
			// as the old one (including attributes).
			//
			// 如果属性发生变化，总是更新子 subconn 的地址。
			// SubConn 执行一个反射。新地址和旧地址的深度相等。
			// 因此，如果当前地址与旧地址相同(包括属性)，那么这就是一个noop。

			// type subConnInfo struct {
			//    subConn balancer.SubConn
			//    attrs   *attributes.Attributes
			// }
			// scInfo 结构体中 subConn 即 ac 的包装对象
			scInfo.attrs = a.Attributes
			b.subConns[aNoAttrs] = scInfo

			// TODO 追源码
			// b.cc 为接口
			// type ClientConn interface {
			//    NewSubConn([]resolver.Address, NewSubConnOptions) (SubConn, error)
			//    RemoveSubConn(SubConn)
			//    UpdateAddresses(SubConn, []resolver.Address)
			//    UpdateState(State)
			//    ResolveNow(resolver.ResolveNowOptions)
			//    Target() string
			// }

			// b.cc 为  balancer_conn_wrappers.go 中 ccBalancerWrapper 结构体

			// 更新地址 包括根据 检查 ac 状态以及 地址，来进行 ac 销毁 或者 重新创建 ac
			b.cc.UpdateAddresses(scInfo.subConn, []resolver.Address{a})
		}
	}

	// ！！！！
	// b.subConns 可能存在 就得已经下掉的地址，所以这里进行判断
	// 如果 地址已经下掉，则 从 b.subConns 中移除
	//
	// 假设 某台通过 etcd 注册的服务端下线了，这个时候 名称解析器会重新解析 并更新地址
	// 更新的时候调用这里 发现已经下线的服务端地址，则会移除
	for a, scInfo := range b.subConns {
		// a was removed by resolver.
		if _, ok := addrsSet[a]; !ok {
			// scInfo.subConn 即 acBalancerWrapper
			b.cc.RemoveSubConn(scInfo.subConn)
			delete(b.subConns, a)
			// Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
			// The entry will be deleted in UpdateSubConnState.
			//
			// 将sc的状态保存在b.scStates中，直到sc的状态变为Shutdown
			// 该条目将在UpdateSubConnState中删除
		}
	}

	// If resolver state contains no addresses, return an error so ClientConn
	// will trigger re-resolve. Also records this as an resolver error, so when
	// the overall state turns transient failure, the error message will have
	// the zero address information.
	//
	// 如果解析器状态不包含地址(没有可用地址)，返回一个错误，因此ClientConn将触发重新解析
	// 还将此记录为解析器错误，因此当整体状态变为瞬态失败时，错误消息将具有零地址信息
	if len(s.ResolverState.Addresses) == 0 {
		// ！！
		b.ResolverError(errors.New("produced zero addresses"))
		return balancer.ErrBadResolverState
	}

	return nil
}

// mergeErrors builds an error from the last connection error and the last
// resolver error.  Must only be called if b.state is TransientFailure.
func (b *baseBalancer) mergeErrors() error {
	// connErr must always be non-nil unless there are no SubConns, in which
	// case resolverErr must be non-nil.
	if b.connErr == nil {
		return fmt.Errorf("last resolver error: %v", b.resolverErr)
	}
	if b.resolverErr == nil {
		return fmt.Errorf("last connection error: %v", b.connErr)
	}
	return fmt.Errorf("last connection error: %v; last resolver error: %v", b.connErr, b.resolverErr)
}

// regeneratePicker takes a snapshot of the balancer, and generates a picker
// from it. The picker is
//  - errPicker if the balancer is in TransientFailure,
//  - built by the pickerBuilder with all READY SubConns otherwise.
//
// 生成负载均衡器 的选择器
func (b *baseBalancer) regeneratePicker() {
	if b.state == connectivity.TransientFailure {
		b.picker = NewErrPicker(b.mergeErrors())
		return
	}

	readySCs := make(map[balancer.SubConn]SubConnInfo)

	// Filter out all ready SCs from full subConn map.
	for addr, scInfo := range b.subConns {
		if st, ok := b.scStates[scInfo.subConn]; ok && st == connectivity.Ready {
			addr.Attributes = scInfo.attrs
			readySCs[scInfo.subConn] = SubConnInfo{Address: addr}
		}
	}

	// 生成 负载均衡器 的 选择器
	b.picker = b.pickerBuilder.Build(PickerBuildInfo{ReadySCs: readySCs})
}

func (b *baseBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	s := state.ConnectivityState

	if logger.V(2) {
		logger.Infof("base.baseBalancer: handle SubConn state change: %p, %v", sc, s)
	}

	oldS, ok := b.scStates[sc]
	if !ok {
		if logger.V(2) {
			logger.Infof("base.baseBalancer: got state changes for an unknown SubConn: %p, %v", sc, s)
		}
		return
	}

	if oldS == connectivity.TransientFailure && s == connectivity.Connecting {
		// Once a subconn enters TRANSIENT_FAILURE, ignore subsequent
		// CONNECTING transitions to prevent the aggregated state from being
		// always CONNECTING when many backends exist but are all down.
		return
	}

	b.scStates[sc] = s

	switch s {
	case connectivity.Idle:
		sc.Connect()

	case connectivity.Shutdown:
		// When an address was removed by resolver, b called RemoveSubConn but
		// kept the sc's state in scStates. Remove state for this sc here.
		delete(b.scStates, sc)

	case connectivity.TransientFailure:
		// Save error to be reported via picker.
		b.connErr = state.ConnectionError
	}

	b.state = b.csEvltr.RecordTransition(oldS, s)

	// Regenerate picker when one of the following happens:
	//  - this sc entered or left ready
	//  - the aggregated state of balancer is TransientFailure
	//    (may need to update error message)

	// 当发生以下情况之一时重新生成选择器:
	// - 这个sc输入 或 准备好了 ？？
	// - 平衡器的聚合状态是TransientFailure
	// 需要更新错误信息
	if (s == connectivity.Ready) != (oldS == connectivity.Ready) ||
		b.state == connectivity.TransientFailure {
		b.regeneratePicker()
	}

	// TODO （read code）
	// 更新 picker
	b.cc.UpdateState(balancer.State{ConnectivityState: b.state, Picker: b.picker})
}

// Close is a nop because base balancer doesn't have internal state to clean up,
// and it doesn't need to call RemoveSubConn for the SubConns.
func (b *baseBalancer) Close() {
}

// NewErrPicker returns a Picker that always returns err on Pick().
// NewErrPicker 返回的 Picker 总是在 Pick() 上返回err
func NewErrPicker(err error) balancer.Picker {
	return &errPicker{err: err}
}

// NewErrPickerV2 is temporarily defined for backward compatibility reasons.
//
// Deprecated: use NewErrPicker instead.
var NewErrPickerV2 = NewErrPicker

type errPicker struct {
	err error // Pick() always returns this err.
}

func (p *errPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{}, p.err
}
