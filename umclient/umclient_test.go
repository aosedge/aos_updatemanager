// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
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

package umclient_test

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	pb "github.com/aosedge/aos_common/api/updatemanager"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/aosedge/aos_updatemanager/config"
	"github.com/aosedge/aos_updatemanager/umclient"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const serverURL = "localhost:8091"

const (
	prepareUpdateMessage = "prepare"
	startUpdateMessage   = "start"
	applyUpdateMessage   = "apply"
	revertUpdateMessage  = "revert"
)

const (
	waitMessageTimeout    = 5 * time.Second
	waitRegisteredTimeout = 30 * time.Second
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

type testServer struct {
	grpcServer      *grpc.Server
	stream          pb.UMService_RegisterUMServer
	registerChannel chan bool
	statusChannel   chan umclient.Status
	pb.UnimplementedUMServiceServer
}

type testMessageHandler struct {
	messageChannel chan string
	components     []umclient.ComponentUpdateInfo
	statusChannel  chan umclient.Status
}

type testCertProvider struct {
	nodeID string
}

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

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

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestMessages(t *testing.T) {
	server, err := newTestServer(serverURL)
	if err != nil {
		t.Fatalf("Can't create test server: %s", err)
	}

	defer server.close()

	handler := newMessageHandler()

	client, err := umclient.New(&config.Config{CMServerURL: serverURL}, handler, newCertProvider("um1"), nil, true)
	if err != nil {
		t.Fatalf("Can't create UM client: %s", err)
	}
	defer client.Close()

	if err = server.waitClientRegistered(); err != nil {
		t.Fatalf("Can't wait client registered: %s", err)
	}

	// Prepare update

	components := []umclient.ComponentUpdateInfo{
		{
			ID: "test1", VendorVersion: "1.0", AosVersion: 1, URL: "url1",
			Annotations: json.RawMessage(`{"id1"}`), Sha256: []byte("sha256Val1"),
			Sha512: []byte("sha512Val1"), Size: 12341,
		},
		{
			ID: "test2", VendorVersion: "2.0", AosVersion: 2, URL: "url2",
			Annotations: json.RawMessage(`{"id1"}`), Sha256: []byte("sha256Val2"),
			Sha512: []byte("sha512Val2"), Size: 12342,
		},
	}

	if err = server.prepareUpdate(components); err != nil {
		t.Fatalf("Can't send prepare update: %s", err)
	}

	if err = handler.waitMessage(prepareUpdateMessage); err != nil {
		t.Errorf("Wait message error: %s", err)
	}

	if !reflect.DeepEqual(handler.components, components) {
		t.Errorf("Wrong components info: %v", handler.components)
	}

	// Start update

	if err = server.startUpdate(); err != nil {
		t.Fatalf("Can't send start update: %s", err)
	}

	if err = handler.waitMessage(startUpdateMessage); err != nil {
		t.Errorf("Wait message error: %s", err)
	}

	// Apply update

	if err = server.applyUpdate(); err != nil {
		t.Fatalf("Can't send apply update: %s", err)
	}

	if err = handler.waitMessage(applyUpdateMessage); err != nil {
		t.Errorf("Wait message error: %s", err)
	}

	// Revert update

	if err = server.revertUpdate(); err != nil {
		t.Fatalf("Can't send revert update: %s", err)
	}

	if err = handler.waitMessage(revertUpdateMessage); err != nil {
		t.Errorf("Wait message error: %s", err)
	}

	// Send status

	componentStatuses := []umclient.ComponentStatusInfo{
		{ID: "test1", Status: umclient.StatusInstalled, VendorVersion: "1.0", AosVersion: 1},
		{ID: "test2", Status: umclient.StatusInstalling, VendorVersion: "2.0", AosVersion: 2, Error: "error"},
	}

	sendStatus := umclient.Status{
		State:      umclient.StateFailed,
		Error:      "Some error occurs",
		Components: componentStatuses,
	}

	handler.sendStatus(sendStatus)

	receiveStatus, err := server.waitStatus()
	if err != nil {
		t.Fatalf("Can't wait status: %s", err)
	}

	if !reflect.DeepEqual(receiveStatus, sendStatus) {
		t.Errorf("Wrong UM status: %v", receiveStatus)
	}
}

func TestServerDisconnect(t *testing.T) {
	server, err := newTestServer(serverURL)
	if err != nil {
		t.Fatalf("Can't create test server: %s", err)
	}

	handler := newMessageHandler()

	client, err := umclient.New(&config.Config{CMServerURL: serverURL}, handler, newCertProvider("um1"), nil, true)
	if err != nil {
		t.Fatalf("Can't create UM client: %s", err)
	}
	defer client.Close()

	if err = server.waitClientRegistered(); err != nil {
		t.Fatalf("Can't wait client registered: %s", err)
	}

	// Disconnect server

	server.close()

	if server, err = newTestServer(serverURL); err != nil {
		t.Fatalf("Can't create test server: %s", err)
	}

	defer server.close()

	if err = server.waitClientRegistered(); err != nil {
		t.Fatalf("Can't wait client registered: %s", err)
	}

	// Prepare update

	if err = server.prepareUpdate([]umclient.ComponentUpdateInfo{
		{ID: "test1", URL: "url1", VendorVersion: "1.0"},
		{ID: "test2", URL: "url2", VendorVersion: "2.0"},
	}); err != nil {
		t.Fatalf("Can't send prepare update: %s", err)
	}

	if err = handler.waitMessage(prepareUpdateMessage); err != nil {
		t.Errorf("Wait message error: %s", err)
	}

	// Send status

	status := umclient.Status{
		State: umclient.StateFailed,
		Error: "Some error occurs",
	}

	handler.sendStatus(status)

	if _, err = server.waitStatus(); err != nil {
		t.Fatalf("Can't wait status: %s", err)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newTestServer(url string) (server *testServer, err error) {
	server = &testServer{registerChannel: make(chan bool), statusChannel: make(chan umclient.Status, 1)}

	listener, err := net.Listen("tcp", url)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	server.grpcServer = grpc.NewServer()

	pb.RegisterUMServiceServer(server.grpcServer, server)

	go func() {
		if err := server.grpcServer.Serve(listener); err != nil {
			log.Errorf("Can't serve grpc server: %s", err)
		}
	}()

	return server, nil
}

func (server *testServer) close() {
	if server.grpcServer != nil {
		server.grpcServer.Stop()
	}
}

func (server *testServer) RegisterUM(stream pb.UMService_RegisterUMServer) (err error) {
	server.stream = stream

	server.registerChannel <- true

	for {
		pbStatus, err := server.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return aoserrors.Wrap(err)
		}

		status := umclient.Status{
			State: umclient.UMState(pbStatus.GetUmState()),
			Error: pbStatus.GetError(),
		}

		for _, component := range pbStatus.GetComponents() {
			status.Components = append(status.Components,
				umclient.ComponentStatusInfo{
					ID:            component.GetId(),
					VendorVersion: component.GetVendorVersion(),
					AosVersion:    component.GetAosVersion(),
					Status:        umclient.ComponentStatus(component.GetStatus()),
					Error:         component.GetError(),
				})
		}

		server.statusChannel <- status
	}
}

func (server *testServer) waitClientRegistered() (err error) {
	select {
	case <-server.registerChannel:
		return nil

	case <-time.After(waitRegisteredTimeout):
		return aoserrors.New("timeout")
	}
}

func (server *testServer) waitStatus() (status umclient.Status, err error) {
	select {
	case status = <-server.statusChannel:
		return status, nil

	case <-time.After(waitMessageTimeout):
		return umclient.Status{}, aoserrors.New("timeout")
	}
}

func (server *testServer) prepareUpdate(components []umclient.ComponentUpdateInfo) (err error) {
	pbComponents := make([]*pb.PrepareComponentInfo, 0, len(components))

	for _, component := range components {
		pbComponents = append(pbComponents, &pb.PrepareComponentInfo{
			Id:            component.ID,
			VendorVersion: component.VendorVersion,
			AosVersion:    component.AosVersion,
			Annotations:   string(component.Annotations),
			Url:           component.URL,
			Sha256:        component.Sha256,
			Sha512:        component.Sha512,
			Size:          component.Size,
		})
	}

	if err = server.stream.Send(&pb.CMMessages{CMMessage: &pb.CMMessages_PrepareUpdate{
		PrepareUpdate: &pb.PrepareUpdate{Components: pbComponents},
	}}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (server *testServer) startUpdate() (err error) {
	if err = server.stream.Send(&pb.CMMessages{CMMessage: &pb.CMMessages_StartUpdate{}}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (server *testServer) applyUpdate() (err error) {
	if err = server.stream.Send(&pb.CMMessages{CMMessage: &pb.CMMessages_ApplyUpdate{}}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (server *testServer) revertUpdate() (err error) {
	if err = server.stream.Send(&pb.CMMessages{CMMessage: &pb.CMMessages_RevertUpdate{}}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func newMessageHandler() (handler *testMessageHandler) {
	handler = &testMessageHandler{messageChannel: make(chan string), statusChannel: make(chan umclient.Status)}

	return handler
}

func (handler *testMessageHandler) Registered() {
}

func (handler *testMessageHandler) PrepareUpdate(components []umclient.ComponentUpdateInfo) {
	handler.messageChannel <- prepareUpdateMessage
	handler.components = components
}

func (handler *testMessageHandler) StartUpdate() {
	handler.messageChannel <- startUpdateMessage
}

func (handler *testMessageHandler) ApplyUpdate() {
	handler.messageChannel <- applyUpdateMessage
}

func (handler *testMessageHandler) RevertUpdate() {
	handler.messageChannel <- revertUpdateMessage
}

func (handler *testMessageHandler) StatusChannel() <-chan umclient.Status {
	return handler.statusChannel
}

func (handler *testMessageHandler) waitMessage(message string) (err error) {
	select {
	case receiveMessage := <-handler.messageChannel:
		if receiveMessage != message {
			return aoserrors.New("wrong message received")
		}

		return nil

	case <-time.After(waitMessageTimeout):
		return aoserrors.New("timeout")
	}
}

func (handler *testMessageHandler) sendStatus(status umclient.Status) {
	handler.statusChannel <- status
}

func newCertProvider(nodeID string) *testCertProvider {
	return &testCertProvider{nodeID: nodeID}
}

func (provider *testCertProvider) GetNodeID() (string, error) {
	return provider.nodeID, nil
}

func (provider *testCertProvider) GetCertificate(certType string) (certURL, ketURL string, err error) {
	return "", "", nil
}
