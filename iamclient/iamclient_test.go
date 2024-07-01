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

	"github.com/aosedge/aos_common/aoserrors"
	pb "github.com/aosedge/aos_common/api/iamanager/v4"
	"github.com/golang/protobuf/ptypes/empty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/aosedge/aos_updatemanager/iamclient"

	"github.com/aosedge/aos_updatemanager/config"
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

	grpcServer *grpc.Server
	nodeID     string
	certURL    certInfo
	keyURL     certInfo
}

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

var server *testServer

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
	var err error

	if server, err = newTestServer(serverURL); err != nil {
		log.Fatalf("Can't create test server: %s", err)
	}

	ret := m.Run()

	server.close()

	os.Exit(ret)
}

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestGetNodeID(t *testing.T) {
	server.nodeID = "testNode"

	client, err := iamclient.New(&config.Config{IAMPublicServerURL: serverURL}, nil, true)
	if err != nil {
		t.Fatalf("Can't create IAM client: %s", err)
	}
	defer client.Close()

	nodeID, err := client.GetNodeID()
	if err != nil {
		t.Fatalf("Can't get node ID: %v", err)
	}

	if nodeID != server.nodeID {
		t.Errorf("Wrong node ID: %s", nodeID)
	}
}

func TestGetCertificates(t *testing.T) {
	server.certURL = certInfo{certType: "um", url: "umCertURL"}
	server.keyURL = certInfo{certType: "um", url: "umKeyURL"}

	client, err := iamclient.New(&config.Config{IAMPublicServerURL: serverURL}, nil, true)
	if err != nil {
		t.Fatalf("Can't create IAM client: %s", err)
	}
	defer client.Close()

	certURL, keyURL, err := client.GetCertificate("um")
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

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func newTestServer(url string) (server *testServer, err error) {
	server = &testServer{}

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

func (server *testServer) GetNodeInfo(context context.Context, req *empty.Empty) (*pb.NodeInfo, error) {
	return &pb.NodeInfo{NodeId: server.nodeID}, nil
}

func (server *testServer) GetCert(context context.Context, req *pb.GetCertRequest) (*pb.GetCertResponse, error) {
	rsp := &pb.GetCertResponse{Type: req.GetType()}

	if req.GetType() != server.certURL.certType {
		return rsp, aoserrors.New("not found")
	}

	if req.GetType() != server.keyURL.certType {
		return rsp, aoserrors.New("not found")
	}

	rsp.CertUrl = server.certURL.url
	rsp.KeyUrl = server.keyURL.url

	return rsp, nil
}
