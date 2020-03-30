// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2019 Renesas Inc.
// Copyright 2019 EPAM Systems Inc.
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

package umserver_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"
	"gitpct.epam.com/nunc-ota/aos_common/wsclient"

	"aos_updatemanager/config"
	"aos_updatemanager/umserver"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const serverURL = "wss://localhost:8088"

// Server creation types
const (
	serverTypeSuccess = iota
	serverTypeFail
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testClient struct {
	wsClient       *wsclient.Client
	messageChannel chan []byte
}

type testUpdater struct {
	status        umprotocol.StatusRsp
	statusChannel chan umprotocol.StatusRsp
}

type testFailUpdater struct {
	status        umprotocol.StatusRsp
	statusChannel chan umprotocol.StatusRsp
}

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	ret := m.Run()

	time.Sleep(time.Second)

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetStatus(t *testing.T) {
	server := newTestServer(serverURL, serverTypeSuccess)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp
	request := umprotocol.StatusReq{}

	if err := client.sendRequest(umprotocol.StatusRequestType, &request, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umprotocol.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}
}

func TestSystemUpgrade(t *testing.T) {
	server := newTestServer(serverURL, serverTypeSuccess)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp
	request := umprotocol.UpgradeReq{ImageVersion: 3}

	if err := client.sendRequest(umprotocol.UpgradeRequestType, &request, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umprotocol.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umprotocol.UpgradeOperation {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

func TestSystemRevert(t *testing.T) {
	server := newTestServer(serverURL, serverTypeSuccess)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp
	request := umprotocol.RevertReq{ImageVersion: 3}

	if err := client.sendRequest(umprotocol.RevertRequestType, &request, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umprotocol.SuccessStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umprotocol.RevertOperation {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

func TestUnsupportedRequest(t *testing.T) {
	server := newTestServer(serverURL, serverTypeSuccess)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp

	request := umprotocol.UpgradeReq{ImageVersion: 3}

	if err = client.sendRequest("WrongRequest", &request, &response, 5*time.Second); err == nil {
		t.Fatal("Error is expected here")
	}

	// Test for the unsupported version
	if err = client.sendVersionedRequest("WrongRequest", &request, &response, 5*time.Second, 255); err == nil {
		t.Fatal("Error is expected here")
	}

	// Test with completely wrong message format
	message := "Some wrong messageo"

	if err = client.sendRawRequest(&message, &response, 5*time.Second); err == nil {
		t.Fatal("Error is expected here")
	}
}

func TestNilDataRequest(t *testing.T) {
	server := newTestServer(serverURL, serverTypeSuccess)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp

	message := umprotocol.Message{
		Header: umprotocol.Header{
			Version:     umprotocol.Version,
			MessageType: umprotocol.UpgradeRequestType,
		},
	}

	message.Data = nil

	if err = client.sendRawRequest(&message, &response, 5*time.Second); err == nil {
		t.Fatal("Expected error here")
	}
}

func TestUpgradeFail(t *testing.T) {
	server := newTestServer(serverURL, serverTypeFail)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp

	request := umprotocol.UpgradeReq{ImageVersion: 3}

	if err := client.sendRequest(umprotocol.UpgradeRequestType, &request, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umprotocol.FailedStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umprotocol.UpgradeOperation {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

func TestRevertFail(t *testing.T) {
	server := newTestServer(serverURL, serverTypeFail)
	defer server.Close()

	client, err := newTestClient(serverURL)
	if err != nil {
		t.Fatalf("Can't create test client: %s", err)
	}
	defer client.close()

	var response umprotocol.StatusRsp

	request := umprotocol.RevertReq{ImageVersion: 3}

	if err := client.sendRequest(umprotocol.RevertRequestType, &request, &response, 5*time.Second); err != nil {
		t.Fatalf("Can't send request: %s", err)
	}

	if response.Status != umprotocol.FailedStatus {
		t.Errorf("Wrong updater status: %s", response.Status)
	}

	if response.Operation != umprotocol.RevertOperation {
		t.Errorf("Wrong operation: %s", response.Operation)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func CreateServerConfig(serverAddress string) (cfg config.Config) {
	configJSON := `{
	"Cert": "../data/crt.pem",
	"Key":  "../data/key.pem"
}`

	decoder := json.NewDecoder(strings.NewReader(configJSON))
	// Parse config
	if err := decoder.Decode(&cfg); err != nil {
		log.Fatalf("Can't parse config: %s", err)
	}

	url, err := url.Parse(serverAddress)
	if err != nil {
		log.Fatalf("Can't parse url: %s", err)
	}

	cfg.ServerURL = url.Host

	return cfg
}

// Success or die
func newTestServer(url string, serverType int) (server *umserver.Server) {
	cfg := CreateServerConfig(serverURL)
	var updater umserver.Updater

	if serverType == serverTypeSuccess {
		updater = &testUpdater{
			status:        umprotocol.StatusRsp{Status: umprotocol.SuccessStatus},
			statusChannel: make(chan umprotocol.StatusRsp)}
	} else {
		updater = &testFailUpdater{
			status:        umprotocol.StatusRsp{Status: umprotocol.SuccessStatus},
			statusChannel: make(chan umprotocol.StatusRsp)}
	}

	server, err := umserver.New(&cfg, updater)
	if err != nil {
		log.Fatalf("Can't create ws server: %s", err)
	}

	// There is raise condition: after new listen is not started yet
	// so we need this delay to wait for listen
	time.Sleep(time.Second)

	return server
}

func newTestClient(url string) (client *testClient, err error) {
	client = &testClient{messageChannel: make(chan []byte, 1)}

	if client.wsClient, err = wsclient.New("TestClient", client.messageHandler); err != nil {
		return nil, err
	}

	if err = client.wsClient.Connect(url); err != nil {
		return nil, err
	}

	return client, nil
}

func (client *testClient) close() {
	client.wsClient.Close()
}

func (client *testClient) messageHandler(message []byte) {
	client.messageChannel <- message
}

func (client *testClient) sendRequest(messageType string, request, response interface{}, timeout time.Duration) (err error) {
	return client.sendVersionedRequest(messageType, request, response, timeout, umprotocol.Version)
}

//NOTE: amoiseev Setting optional parameter to be able to set wrong version in header
func (client *testClient) sendVersionedRequest(messageType string, request, response interface{}, timeout time.Duration, version uint64) (err error) {
	message := umprotocol.Message{
		Header: umprotocol.Header{
			Version:     version,
			MessageType: messageType,
		},
	}

	if message.Data, err = json.Marshal(request); err != nil {
		return err
	}

	return client.sendRawRequest(message, response, timeout)
}

func (client *testClient) sendRawRequest(message, response interface{}, timeout time.Duration) (err error) {

	if err = client.wsClient.SendMessage(&message); err != nil {
		return err
	}

	select {
	case <-time.After(timeout):
		return errors.New("wait response timeout")

	case messageJSON := <-client.messageChannel:
		var message umprotocol.Message

		if err = json.Unmarshal(messageJSON, &message); err != nil {
			return err
		}

		if err = json.Unmarshal(message.Data, response); err != nil {
			return err
		}
	}

	return nil
}

func (updater *testUpdater) GetStatus() (status umprotocol.StatusRsp) {
	return updater.status
}

func (updater *testUpdater) Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error) {
	updater.status.CurrentVersion = version
	updater.status.Operation = umprotocol.UpgradeOperation
	updater.status.Status = umprotocol.SuccessStatus

	updater.statusChannel <- updater.status

	return nil
}

func (updater *testUpdater) Revert(version uint64) (err error) {
	updater.status.CurrentVersion = version
	updater.status.Operation = umprotocol.RevertOperation
	updater.status.Status = umprotocol.SuccessStatus

	updater.statusChannel <- updater.status

	return nil
}

func (updater *testUpdater) StatusChannel() (statusChannel <-chan umprotocol.StatusRsp) {
	return updater.statusChannel
}

/*******************************************************************************
 * testFailUpdater implementation
 ******************************************************************************/

func (updater *testFailUpdater) GetStatus() (status umprotocol.StatusRsp) {
	return updater.status
}

func (updater *testFailUpdater) Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error) {
	updater.status.CurrentVersion = version
	updater.status.Operation = umprotocol.UpgradeOperation
	updater.status.Status = umprotocol.FailedStatus
	updater.status.Error = "UnitTest: failed update"

	return errors.New(updater.status.Error)
}

func (updater *testFailUpdater) Revert(version uint64) (err error) {
	updater.status.CurrentVersion = version
	updater.status.Operation = umprotocol.RevertOperation
	updater.status.Status = umprotocol.FailedStatus
	updater.status.Error = "UnitTest: failed revert"

	return errors.New(updater.status.Error)
}

func (updater *testFailUpdater) StatusChannel() (statusChannel <-chan umprotocol.StatusRsp) {
	return updater.statusChannel
}
