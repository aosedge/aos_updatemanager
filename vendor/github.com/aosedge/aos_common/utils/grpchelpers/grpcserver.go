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
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/utils/cryptutils"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// RegisteredService registered service information.
type RegisteredService struct {
	desc *grpc.ServiceDesc
	impl interface{}
}

// GRPCServer implementation restarts GRPC server on every server options update.
type GRPCServer struct {
	sync.Mutex

	serverAddress string

	grpcServer *grpc.Server
	services   []RegisteredService

	stopWG sync.WaitGroup
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// NewGRPCServer creates a new instance of GRPCServer.
func NewGRPCServer(address string) *GRPCServer {
	return &GRPCServer{
		serverAddress: address,
		services:      make([]RegisteredService, 0),
	}
}

// NewProtectedServerOptions creates protected server options.
func NewProtectedServerOptions(cryptocontext *cryptutils.CryptoContext, certProvider CertProvider,
	certType string, insecureConn bool,
) ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption

	if !insecureConn {
		certURL, keyURL, err := certProvider.GetCertificate(certType, nil, "")
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		tlsConfig, err := cryptocontext.GetClientMutualTLSConfig(certURL, keyURL)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	return opts, nil
}

// RegisterService registers a new grpc service.
func (server *GRPCServer) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	server.Lock()
	defer server.Unlock()

	server.services = append(server.services, RegisteredService{desc, impl})
}

// StartServer starts the gRPC server with new options.
func (server *GRPCServer) RestartServer(options []grpc.ServerOption) error {
	server.Lock()
	defer server.Unlock()

	server.stopGRPCServer()

	return server.startGRPCServer(options)
}

// Stop shuts down the server.
func (server *GRPCServer) StopServer() {
	server.Lock()
	defer server.Unlock()

	log.WithField("serverURL", server.serverAddress).Debug("Stop GRPC server")

	server.stopGRPCServer()
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (server *GRPCServer) stopGRPCServer() {
	if server.grpcServer != nil {
		server.grpcServer.Stop()
	}

	server.stopWG.Wait()

	server.grpcServer = nil
}

func (server *GRPCServer) startGRPCServer(options []grpc.ServerOption) error {
	log.WithField("serverURL", server.serverAddress).Debug("Start GRPC server")

	listener, err := net.Listen("tcp", server.serverAddress)
	if err != nil {
		log.Errorf("Failed to listen: %v", err)

		return aoserrors.Wrap(err)
	}

	server.grpcServer = grpc.NewServer(options...)

	for _, service := range server.services {
		server.grpcServer.RegisterService(service.desc, service.impl)
	}

	server.stopWG.Add(1)

	go func() {
		defer server.stopWG.Done()

		if err := server.grpcServer.Serve(listener); err != nil {
			log.Errorf("Failed to serve: %v", err)
		}
	}()

	return nil
}
