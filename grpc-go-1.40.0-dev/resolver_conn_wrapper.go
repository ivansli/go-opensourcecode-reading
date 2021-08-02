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

package grpc

import (
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ccResolverWrapper is a wrapper on top of cc for resolvers.
// It implements resolver.ClientConn interface.
//
// ccResolverWrapper 是一个封装在cc之上的解析器
// 它实现了 resolver.ClientConn 接口
type ccResolverWrapper struct {
	cc         *ClientConn
	resolverMu sync.Mutex
	resolver   resolver.Resolver
	done       *grpcsync.Event
	curState   resolver.State

	incomingMu sync.Mutex // Synchronizes all the incoming calls.
}

// newCCResolverWrapper uses the resolver.Builder to build a Resolver and
// returns a ccResolverWrapper object which wraps the newly built resolver.
//
// 使用 resolver.Builder 取创建一个 Resolver 并且返回一个被包裹的对象 ccResolverWrapper
func newCCResolverWrapper(cc *ClientConn, rb resolver.Builder) (*ccResolverWrapper, error) {
	// 对 名称解析器对象、解析出的服务端列表、客户端连接对象 的封装
	ccr := &ccResolverWrapper{
		cc:   cc,
		done: grpcsync.NewEvent(),
	}

	var credsClone credentials.TransportCredentials
	if creds := cc.dopts.copts.TransportCredentials; creds != nil {
		credsClone = creds.Clone()
	}

	// 名称解析器的构建器选项
	rbo := resolver.BuildOptions{
		DisableServiceConfig: cc.dopts.disableServiceConfig,
		DialCreds:            credsClone,
		CredsBundle:          cc.dopts.copts.CredsBundle,
		Dialer:               cc.dopts.copts.Dialer,
	}

	var err error
	// We need to hold the lock here while we assign to the ccr.resolver field
	// to guard against a data race caused by the following code path,
	// rb.Build-->ccr.ReportError-->ccr.poll-->ccr.resolveNow, would end up
	// accessing ccr.resolver which is being assigned here.
	//
	// 在给 ccr.resolver 赋值的时候，我们应该为了防止数据竞争予以加锁保护
	ccr.resolverMu.Lock()
	defer ccr.resolverMu.Unlock()

	///////////////////////////////////////////////////////////////////////
	// 调用 resolver.Builder 的 Build 方法
	// 以 passthrough internal/resolver/passthrough/passthrough.go 为例
	// ！！！核心
	//
	// 通过 名称解析器构建器 创建一个新的 名称解析器对象
	// ccr.resolver 存储 名称解析器 对象
	//
	// rb.Build() 方法
	// 1. 通过服务名称解析出来对应的服务端地址列表 并封装成 resolver.State{Addresses: addrs} 对象
	// 2. 调用 conn 的 go-opensourcecode-read/grpc-go/clientconn.go 的 UpdateState()方法
	// 	  把地址列表 resolver.State 更新到 cc 的 负载均衡对象 balancer.ClientConnState 中
	///////////////////////////////////////////////////////////////////////
	// 这里的 Build 调用的是名称解析器构建器的 Build 方法
	ccr.resolver, err = rb.Build(cc.parsedTarget, ccr, rbo)

	if err != nil {
		return nil, err
	}
	return ccr, nil
}

func (ccr *ccResolverWrapper) resolveNow(o resolver.ResolveNowOptions) {
	ccr.resolverMu.Lock()
	if !ccr.done.HasFired() {
		ccr.resolver.ResolveNow(o)
	}
	ccr.resolverMu.Unlock()
}

func (ccr *ccResolverWrapper) close() {
	ccr.resolverMu.Lock()
	ccr.resolver.Close()
	ccr.done.Fire()
	ccr.resolverMu.Unlock()
}

// ！！！更新状态，包含建立连接的逻辑
//
// s resolver.State : 为解析器解析出来的所有可用地址
func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error {
	ccr.incomingMu.Lock()
	defer ccr.incomingMu.Unlock()

	if ccr.done.HasFired() {
		return nil
	}

	// 记录日志
	channelz.Infof(logger, ccr.cc.channelzID, "ccResolverWrapper: sending update to cc: %v", s)
	if channelz.IsOn() {
		ccr.addChannelzTraceEvent(s)
	}

	ccr.curState = s

	// ！！！核心
	// 更新状态并建立连接
	//
	// clientconn.go 中 updateResolverState 方法
	// ccr.curState 包含所有解析出来的服务端地址列表
	if err := ccr.cc.updateResolverState(ccr.curState, nil); err == balancer.ErrBadResolverState {
		return balancer.ErrBadResolverState
	}
	return nil
}

func (ccr *ccResolverWrapper) ReportError(err error) {
	ccr.incomingMu.Lock()
	defer ccr.incomingMu.Unlock()
	if ccr.done.HasFired() {
		return
	}
	channelz.Warningf(logger, ccr.cc.channelzID, "ccResolverWrapper: reporting error to cc: %v", err)
	ccr.cc.updateResolverState(resolver.State{}, err)
}

// NewAddress is called by the resolver implementation to send addresses to gRPC.
func (ccr *ccResolverWrapper) NewAddress(addrs []resolver.Address) {
	ccr.incomingMu.Lock()
	defer ccr.incomingMu.Unlock()
	if ccr.done.HasFired() {
		return
	}
	channelz.Infof(logger, ccr.cc.channelzID, "ccResolverWrapper: sending new addresses to cc: %v", addrs)
	if channelz.IsOn() {
		ccr.addChannelzTraceEvent(resolver.State{Addresses: addrs, ServiceConfig: ccr.curState.ServiceConfig})
	}
	ccr.curState.Addresses = addrs
	ccr.cc.updateResolverState(ccr.curState, nil)
}

// NewServiceConfig is called by the resolver implementation to send service
// configs to gRPC.
func (ccr *ccResolverWrapper) NewServiceConfig(sc string) {
	ccr.incomingMu.Lock()
	defer ccr.incomingMu.Unlock()
	if ccr.done.HasFired() {
		return
	}
	channelz.Infof(logger, ccr.cc.channelzID, "ccResolverWrapper: got new service config: %v", sc)
	if ccr.cc.dopts.disableServiceConfig {
		channelz.Info(logger, ccr.cc.channelzID, "Service config lookups disabled; ignoring config")
		return
	}
	scpr := parseServiceConfig(sc)
	if scpr.Err != nil {
		channelz.Warningf(logger, ccr.cc.channelzID, "ccResolverWrapper: error parsing service config: %v", scpr.Err)
		return
	}
	if channelz.IsOn() {
		ccr.addChannelzTraceEvent(resolver.State{Addresses: ccr.curState.Addresses, ServiceConfig: scpr})
	}
	ccr.curState.ServiceConfig = scpr
	ccr.cc.updateResolverState(ccr.curState, nil)
}

func (ccr *ccResolverWrapper) ParseServiceConfig(scJSON string) *serviceconfig.ParseResult {
	return parseServiceConfig(scJSON)
}

func (ccr *ccResolverWrapper) addChannelzTraceEvent(s resolver.State) {
	var updates []string
	var oldSC, newSC *ServiceConfig
	var oldOK, newOK bool
	if ccr.curState.ServiceConfig != nil {
		oldSC, oldOK = ccr.curState.ServiceConfig.Config.(*ServiceConfig)
	}
	if s.ServiceConfig != nil {
		newSC, newOK = s.ServiceConfig.Config.(*ServiceConfig)
	}
	if oldOK != newOK || (oldOK && newOK && oldSC.rawJSONString != newSC.rawJSONString) {
		updates = append(updates, "service config updated")
	}
	if len(ccr.curState.Addresses) > 0 && len(s.Addresses) == 0 {
		updates = append(updates, "resolver returned an empty address list")
	} else if len(ccr.curState.Addresses) == 0 && len(s.Addresses) > 0 {
		updates = append(updates, "resolver returned new addresses")
	}
	channelz.AddTraceEvent(logger, ccr.cc.channelzID, 0, &channelz.TraceEventDesc{
		Desc:     fmt.Sprintf("Resolver state updated: %+v (%v)", s, strings.Join(updates, "; ")),
		Severity: channelz.CtInfo,
	})
}
