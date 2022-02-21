// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2022 Renesas Electronics Corporation.
// Copyright (C) 2022 EPAM Systems, Inc.
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

package iamclient_test

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/aoscloud/aos_common/aoserrors"
	pb "github.com/aoscloud/aos_common/api/iamanager/v2"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/aoscloud/aos_updatemanager/iamclient"

	"github.com/aoscloud/aos_updatemanager/config"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const serverURL = "localhost:8090"

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

type certInfo struct {
	certType string
	url      string
}

type testServer struct {
	pb.UnimplementedIAMPublicServiceServer

	grpcServer          *grpc.Server
	usersChangedChannel chan []string
	certURL             certInfo
	keyURL              certInfo
}

/***********************************************************************************************************************
 * Init
 **********************************************************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/***********************************************************************************************************************
 * Main
 **********************************************************************************************************************/

func TestMain(m *testing.M) {
	ret := m.Run()

	os.Exit(ret)
}

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestGetCertificates(t *testing.T) {
	server, err := newTestServer(serverURL)
	if err != nil {
		t.Fatalf("Can't create test server: %s", err)
	}

	defer server.close()

	server.certURL = certInfo{certType: "um", url: "umCertURL"}
	server.keyURL = certInfo{certType: "um", url: "umKeyURL"}

	client, err := iamclient.New(&config.Config{IAMServerURL: serverURL}, true)
	if err != nil {
		t.Fatalf("Can't create IAM client: %s", err)
	}

	certURL, keyURL, err := client.GetCertificate("um", nil)
	if err != nil {
		t.Fatalf("Can't get um certificate: %s", err)
	}

	if certURL != server.certURL.url {
		t.Fatalf("Wrong um cert URL: %s", certURL)
	}

	if keyURL != server.keyURL.url {
		t.Fatalf("Wrong um key URL: %s", keyURL)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newTestServer(url string) (server *testServer, err error) {
	server = &testServer{usersChangedChannel: make(chan []string, 1)}

	listener, err := net.Listen("tcp", url)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	server.grpcServer = grpc.NewServer()

	pb.RegisterIAMPublicServiceServer(server.grpcServer, server)

	go func() {
		if err := server.grpcServer.Serve(listener); err != nil {
			log.Warn(err)
		}
	}()

	return server, nil
}

func (server *testServer) close() {
	if server.grpcServer != nil {
		server.grpcServer.Stop()
	}
}

func (server *testServer) GetCert(
	context context.Context, req *pb.GetCertRequest) (rsp *pb.GetCertResponse, err error) {
	rsp = &pb.GetCertResponse{Type: req.Type}

	if req.Type != server.certURL.certType {
		return rsp, aoserrors.New("not found")
	}

	if req.Type != server.keyURL.certType {
		return rsp, aoserrors.New("not found")
	}

	rsp.CertUrl = server.certURL.url
	rsp.KeyUrl = server.keyURL.url

	return rsp, nil
}
