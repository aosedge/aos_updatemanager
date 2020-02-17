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

package wsclient_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"aos_common/wsclient"
	"aos_common/wsserver"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const hostURL = ":8088"
const serverURL = "wss://localhost:8088"
const crtFile = "../wsserver/data/crt.pem"
const keyFile = "../wsserver/data/key.pem"

/*******************************************************************************
 * Types
 ******************************************************************************/

/*******************************************************************************
 * Vars
 ******************************************************************************/

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

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestSendRequest(t *testing.T) {
	type Header struct {
		Type      string
		RequestID string
	}

	type Request struct {
		Header Header
		Value  int
	}

	type Response struct {
		Header Header
		Value  float32
		Error  *string `json:"Error,omitempty"`
	}

	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile,
		func(messageType int, data []byte) (response []byte, err error) {
			var req Request
			var rsp Response

			if err = json.Unmarshal(data, &req); err != nil {
				return nil, err
			}

			rsp.Header.Type = req.Header.Type
			rsp.Header.RequestID = req.Header.RequestID
			rsp.Value = float32(req.Value) / 10.0

			if response, err = json.Marshal(rsp); err != nil {
				return
			}

			return response, nil
		})
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", nil)
	if err != nil {
		t.Fatalf("Can't create ws client: %s", err)
	}
	defer client.Close()

	if err = client.Connect(serverURL); err != nil {
		t.Fatalf("Can't connect to ws server: %s", err)
	}

	req := Request{Header: Header{Type: "GET", RequestID: uuid.New().String()}}
	rsp := Response{}

	if err = client.SendRequest("Header.RequestID", req.Header.RequestID, &req, &rsp); err != nil {
		t.Errorf("Can't send request: %s", err)
	}

	if rsp.Header.Type != req.Header.Type {
		t.Errorf("Wrong response type: %s", rsp.Header.Type)
	}
}

func TestWrongIDRequest(t *testing.T) {
	type Request struct {
		Type      string
		RequestID string
		Value     int
	}

	type Response struct {
		Type      string
		RequestID string
		Value     float32
		Error     *string `json:"Error,omitempty"`
	}

	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile,
		func(messageType int, data []byte) (response []byte, err error) {
			var req Request
			var rsp Response

			if err = json.Unmarshal(data, &req); err != nil {
				return nil, err
			}

			rsp.Type = req.Type
			rsp.RequestID = uuid.New().String()
			rsp.Value = float32(req.Value) / 10.0

			if response, err = json.Marshal(rsp); err != nil {
				return
			}

			return response, err
		})
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", nil)
	if err != nil {
		t.Fatalf("Can't create ws client: %s", err)
	}
	defer client.Close()

	if err = client.Connect(serverURL); err != nil {
		t.Fatalf("Can't connect to ws server: %s", err)
	}

	req := Request{Type: "GET", RequestID: uuid.New().String()}
	rsp := Response{}

	if err = client.SendRequest("RequestID", req.RequestID, &req, &rsp); err == nil {
		t.Error("Error expected")
	}
}

func TestErrorChannel(t *testing.T) {
	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile, nil)
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", nil)
	if err != nil {
		t.Fatalf("Can't create ws client: %s", err)
	}
	defer client.Close()

	if err = client.Connect(serverURL); err != nil {
		t.Fatalf("Can't connect to ws server: %s", err)
	}

	server.Close()

	select {
	case <-client.ErrorChannel:

	case <-time.After(5 * time.Second):
		t.Error("Waiting error channel timeout")
	}
}

func TestMessageHandler(t *testing.T) {
	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile, nil)
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	type Message struct {
		Type  string
		Value int
	}

	messageChannel := make(chan Message)

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", func(data []byte) {
		var message Message

		if err := json.Unmarshal(data, &message); err != nil {
			t.Errorf("Parse message error: %s", err)
			return
		}

		messageChannel <- message
	})
	if err != nil {
		t.Fatalf("Can't create ws client: %s", err)
	}

	if err = client.Connect(serverURL); err != nil {
		t.Fatalf("Can't connect to ws server: %s", err)
	}
	defer client.Close()

	clientHandlers := server.GetClients()
	if len(clientHandlers) == 0 {
		t.Fatalf("No connected clients")
	}

	for _, clientHandler := range clientHandlers {
		if err = clientHandler.SendMessage(websocket.TextMessage, []byte(`{"Type":"NOTIFY", "Value": 123}`)); err != nil {
			t.Fatalf("Can't send message: %s", err)
		}
	}

	select {
	case message := <-messageChannel:
		if message.Type != "NOTIFY" || message.Value != 123 {
			t.Error("Wrong message value")
		}

	case <-time.After(5 * time.Second):
		t.Error("Waiting message timeout")
	}
}

func TestSendMessage(t *testing.T) {
	type Message struct {
		Type  string
		Value int
	}

	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile,
		func(messageType int, data []byte) (response []byte, err error) {
			return data, nil
		})
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	messageChannel := make(chan Message)

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", func(data []byte) {
		var message Message

		if err := json.Unmarshal(data, &message); err != nil {
			t.Errorf("Parse message error: %s", err)
			return
		}

		messageChannel <- message
	})
	if err != nil {
		t.Fatalf("Error create a new ws client")
	}

	// Send message to server before connect
	if err := client.SendMessage(&Message{Type: "NOTIFY", Value: 123}); err == nil {
		t.Error("Expect error because client is not connected")
	}

	if err = client.Connect(serverURL); err != nil {
		t.Fatalf("Can't connect to ws server: %s", err)
	}
	defer client.Close()

	if err := client.SendMessage(&Message{Type: "NOTIFY", Value: 123}); err != nil {
		t.Errorf("Error sending message form client: %s", err)
	}

	select {
	case message := <-messageChannel:
		if message.Type != "NOTIFY" || message.Value != 123 {
			t.Error("Wrong message value")
		}

	case <-time.After(5 * time.Second):
		t.Error("Waiting message timeout")
	}
}

func TestConnectDisconnect(t *testing.T) {
	server, err := wsserver.New("TestServer", hostURL, crtFile, keyFile, nil)
	if err != nil {
		t.Fatalf("Can't create ws server: %s", err)
	}
	defer server.Close()

	time.Sleep(1 * time.Second)

	client, err := wsclient.New("Test", nil)
	if err != nil {
		t.Fatalf("Can't create ws client: %s", err)
	}
	defer client.Close()

	if err = client.Connect(serverURL); err != nil {
		t.Errorf("Can't connect to ws server: %s", err)
	}

	if err = client.Connect(serverURL); err == nil {
		t.Error("Expect error because client is connected")
	}

	if err = client.Disconnect(); err != nil {
		t.Errorf("Can't disconnect client: %s", err)
	}

	if client.IsConnected() == true {
		t.Error("Client should not be connected")
	}

	if err = client.Connect(serverURL); err != nil {
		t.Errorf("Can't connect to ws server: %s", err)
	}

	if len(server.GetClients()) == 0 {
		t.Error("No connected clients")
	}

	if client.IsConnected() != true {
		t.Error("Client should be connected")
	}
}
