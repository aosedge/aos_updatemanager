// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpchelpers

import (
	"context"
	"sync"

	"github.com/aosedge/aos_common/aoserrors"
	"google.golang.org/grpc"
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// GRPCConn is a wrapper for grpc connection that blocks all incoming requests for stopped connection.
// It is used to recover connection without canceling grpc requests.
type GRPCConn struct {
	sync.Mutex

	grpcConn    *grpc.ClientConn
	connStarted sync.WaitGroup
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// NewGRPCConn creates new GRPCConn object.
func NewGRPCConn() *GRPCConn {
	conn := GRPCConn{}

	conn.connStarted.Add(1)

	return &conn
}

// Start starts new grpc connection.
func (conn *GRPCConn) Start(connection *grpc.ClientConn) error {
	conn.Lock()
	defer conn.Unlock()

	if conn.grpcConn != nil {
		return aoserrors.New("Previous connection should be stopped before reset")
	}

	conn.grpcConn = connection
	conn.connStarted.Done()

	return nil
}

// Stop stops current grpc connection.
func (conn *GRPCConn) Stop() {
	conn.Lock()
	defer conn.Unlock()

	if conn.grpcConn == nil {
		return
	}

	conn.grpcConn.Close()
	conn.grpcConn = nil

	conn.connStarted.Add(1)
}

// Close closes grpc connection and releases all spawned goroutines.
func (conn *GRPCConn) Close() {
	conn.Lock()
	defer conn.Unlock()

	if conn.grpcConn == nil {
		conn.connStarted.Done()
	} else {
		conn.grpcConn.Close()
		conn.grpcConn = nil
	}
}

/***********************************************************************************************************************
 * grpc.ClientConnInterface interface implementation
 **********************************************************************************************************************/

// Invoke performs a unary RPC and returns after the response is received into reply.
func (conn *GRPCConn) Invoke(ctx context.Context, method string, args any, reply any,
	opts ...grpc.CallOption,
) error {
	lock := make(chan struct{}, 1)

	go func() {
		conn.connStarted.Wait()
		lock <- struct{}{}
	}()

	select {
	case <-lock:
		conn.Lock()
		defer conn.Unlock()

		if conn.grpcConn == nil {
			return aoserrors.New("grpc connection closed")
		}

		return aoserrors.Wrap(conn.grpcConn.Invoke(ctx, method, args, reply, opts...))

	case <-ctx.Done():
		return aoserrors.New("grpc context closed")
	}
}

// NewStream begins a streaming RPC.
func (conn *GRPCConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	lock := make(chan struct{}, 1)

	go func() {
		conn.connStarted.Wait()
		lock <- struct{}{}
	}()

	select {
	case <-lock:
		conn.Lock()
		defer conn.Unlock()

		if conn.grpcConn == nil {
			return nil, aoserrors.New("grpc connection closed")
		}

		stream, err := conn.grpcConn.NewStream(ctx, desc, method, opts...)

		return stream, aoserrors.Wrap(err)

	case <-ctx.Done():
		return nil, aoserrors.New("grpc context closed")
	}
}
