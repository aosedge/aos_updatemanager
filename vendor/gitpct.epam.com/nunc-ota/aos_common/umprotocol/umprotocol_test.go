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

package umprotocol_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"aos_common/umprotocol"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var messageMap = map[string]func() interface{}{
	umprotocol.UpgradeRequestType: func() interface{} { return &umprotocol.UpgradeReq{} },
	umprotocol.RevertRequestType:  func() interface{} { return &umprotocol.RevertReq{} },
	umprotocol.StatusRequestType:  func() interface{} { return &umprotocol.StatusReq{} },
	umprotocol.StatusResponseType: func() interface{} { return &umprotocol.StatusRsp{} },
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestMessages(t *testing.T) {
	type TestMessage struct {
		messageType string
		messageData interface{}
	}

	testData := []TestMessage{
		{
			umprotocol.UpgradeRequestType,
			&umprotocol.UpgradeReq{
				ImageVersion: 34,
				ImageInfo: umprotocol.ImageInfo{
					Path:   "/this/is/path",
					Sha256: []byte("1234"),
					Sha512: []byte("5678"),
					Size:   4567},
			},
		},
		{
			umprotocol.RevertRequestType,
			&umprotocol.RevertReq{
				ImageVersion: 34,
			},
		},
		{
			umprotocol.StatusRequestType,
			&umprotocol.StatusReq{},
		},
		{
			umprotocol.StatusResponseType,
			&umprotocol.StatusRsp{
				Operation:        umprotocol.RevertOperation,
				Status:           umprotocol.FailedStatus,
				Error:            "this is serious error",
				RequestedVersion: 13,
				CurrentVersion:   344,
			},
		},
	}

	for _, test := range testData {
		messageJSON, err := marshalMessage(test.messageType, test.messageData)
		if err != nil {
			t.Errorf("Can't marshal message: %s", err)
		}

		messageType, data, err := unmarshalMessage(messageJSON)
		if err != nil {
			t.Errorf("Can't unmarshal message: %s", err)
		}

		if messageType != test.messageType {
			t.Errorf("wrong message type: %s", test.messageType)
		}

		if !reflect.DeepEqual(test.messageData, data) {
			t.Errorf("message mistmatch: %v != %v", test.messageData, data)
		}
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func marshalMessage(messageType string, data interface{}) (messageJSON []byte, err error) {
	message := umprotocol.Message{
		Header: umprotocol.Header{
			Version:     umprotocol.Version,
			MessageType: messageType}}

	if message.Data, err = json.Marshal(data); err != nil {
		return nil, err
	}

	if messageJSON, err = json.Marshal(&message); err != nil {
		return nil, err
	}

	return messageJSON, err
}

func unmarshalMessage(messageJSON []byte) (messageType string, data interface{}, err error) {
	var message umprotocol.Message

	if err = json.Unmarshal(messageJSON, &message); err != nil {
		return "", nil, err
	}

	if message.Header.Version != umprotocol.Version {
		return "", nil, errors.New("unsupported message version")
	}

	getType, ok := messageMap[message.Header.MessageType]
	if !ok {
		return message.Header.MessageType, nil, errors.New("unsupported message type")
	}

	data = getType()

	if err = json.Unmarshal(message.Data, data); err != nil {
		return message.Header.MessageType, nil, err
	}

	return message.Header.MessageType, data, nil
}
