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

// Package connectivity defines connectivity semantics.
// For details, see https://github.com/grpc/grpc/blob/master/doc/connectivity-semantics-and-api.md.
// All APIs in this package are experimental.
package connectivity

import (
	"google.golang.org/grpc/grpclog"
)

var logger = grpclog.Component("core")

// State indicates the state of connectivity.
// It can be the state of a ClientConn or SubConn.
type State int

func (s State) String() string {
	switch s {
	case Idle:
		return "IDLE"
	case Connecting:
		return "CONNECTING"
	case Ready:
		return "READY"
	case TransientFailure:
		return "TRANSIENT_FAILURE"
	case Shutdown:
		return "SHUTDOWN"
	default:
		logger.Errorf("unknown connectivity state: %d", s)
		return "Invalid-State"
	}
}

const (
	// Idle indicates the ClientConn is idle.
	Idle State = iota

	// Connecting indicates the ClientConn is connecting.
	// 建立连接中
	Connecting

	// Ready indicates the ClientConn is ready for work.
	// 就绪
	Ready

	// TransientFailure indicates the ClientConn has seen a failure but expects to recover.
	// 表示ClientConn看到了一个失败，但预期会恢复。
	TransientFailure

	// Shutdown indicates the ClientConn has started shutting down.
	// 表示ClientConn已开始关闭。
	Shutdown
)
