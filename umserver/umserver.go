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

package umserver

import (
	"encoding/json"
	"errors"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"
	"gitpct.epam.com/nunc-ota/aos_common/wsserver"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// Server update manager server structure
type Server struct {
	wsServer *wsserver.Server
	updater  Updater
}

// Updater interface
type Updater interface {
	GetCurrentVersion() (version uint64)
	GetOperationVersion() (version uint64)
	GetLastOperation() (operation string)
	GetStatus() (status string)
	SetStatusCallback(callback func(status string))
	GetLastError() (err error)
	Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error)
	Revert(version uint64) (err error)
}

type messageProcessor struct {
	updater     Updater
	sendMessage wsserver.SendMessage
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new Web socket server
func New(cfg *config.Config, updater Updater) (server *Server, err error) {
	server = &Server{updater: updater}

	if server.wsServer, err = wsserver.New("UM", cfg.ServerURL, cfg.Cert, cfg.Key, server.newMessageProcessor); err != nil {
		return nil, err
	}

	return server, nil
}

// Close closes web socket server and all connections
func (server *Server) Close() {
	server.wsServer.Close()
}

// ProcessMessage proccess incoming messages
func (processor *messageProcessor) ProcessMessage(messageType int, messageJSON []byte) (response []byte, err error) {
	if messageType != websocket.TextMessage {
		return nil, errors.New("incoming message in unsupported format")
	}

	var message umprotocol.Message

	if err = json.Unmarshal(messageJSON, &message); err != nil {
		return nil, err
	}

	if message.Header.Version != umprotocol.Version {
		return nil, errors.New("unsupported message version")
	}

	switch string(message.Header.MessageType) {
	case umprotocol.StatusRequestType:
		return processor.processGetStatus()

	case umprotocol.UpgradeRequestType:
		return processor.processSystemUpgrade(message.Data)

	case umprotocol.RevertRequestType:
		return processor.processSystemRevert(message.Data)

	default:
		return nil, errors.New("unsupported request type: " + message.Header.MessageType)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *Server) newMessageProcessor(sendMessage wsserver.SendMessage) (processor wsserver.MessageProcessor, err error) {
	messageProcessor := &messageProcessor{updater: server.updater, sendMessage: sendMessage}

	messageProcessor.updater.SetStatusCallback(messageProcessor.sendStatus)

	return messageProcessor, nil
}

func (processor *messageProcessor) sendStatus(status string) {
	errStr := ""

	if err := processor.updater.GetLastError(); err != nil {
		errStr = err.Error()
	}

	statusMessage := umprotocol.StatusRsp{
		Operation:        processor.updater.GetLastOperation(),
		Status:           status,
		Error:            errStr,
		RequestedVersion: processor.updater.GetOperationVersion(),
		CurrentVersion:   processor.updater.GetCurrentVersion(),
	}

	log.Debug("Send operation status")

	statusJSON, err := marshalResponse(umprotocol.StatusResponseType, &statusMessage)
	if err != nil {
		log.Errorf("Can't marshal status message: %s", err)
	}

	if err = processor.sendMessage(websocket.TextMessage, statusJSON); err != nil {
		log.Errorf("Can't send status message: %s", err)
	}
}

func (processor *messageProcessor) processGetStatus() (response []byte, err error) {
	errStr := ""

	if err = processor.updater.GetLastError(); err != nil {
		errStr = err.Error()
	}

	statusMessage := umprotocol.StatusRsp{
		Operation:        processor.updater.GetLastOperation(),
		Status:           processor.updater.GetStatus(),
		Error:            errStr,
		RequestedVersion: processor.updater.GetOperationVersion(),
		CurrentVersion:   processor.updater.GetCurrentVersion(),
	}

	log.Debug("Process get status request")

	return marshalResponse(umprotocol.StatusResponseType, &statusMessage)
}

func (processor *messageProcessor) processSystemUpgrade(request []byte) (response []byte, err error) {
	var upgradeReq umprotocol.UpgradeReq

	if err = json.Unmarshal(request, &upgradeReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", upgradeReq.ImageVersion).Debug("Process system upgrade request")

	if err := processor.updater.Upgrade(upgradeReq.ImageVersion, upgradeReq.ImageInfo); err != nil {
		log.Errorf("Upgrade failed: %s", err)

		statusMessage := umprotocol.StatusRsp{
			Status:           umprotocol.FailedStatus,
			Operation:        umprotocol.UpgradeOperation,
			RequestedVersion: upgradeReq.ImageVersion,
			CurrentVersion:   processor.updater.GetCurrentVersion(),
			Error:            err.Error(),
		}

		return marshalResponse(umprotocol.StatusResponseType, &statusMessage)
	}

	return nil, nil
}

func (processor *messageProcessor) processSystemRevert(request []byte) (response []byte, err error) {
	var revertReq umprotocol.RevertReq

	if err = json.Unmarshal(request, &revertReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", revertReq.ImageVersion).Debug("Process system revert request")

	if err := processor.updater.Revert(revertReq.ImageVersion); err != nil {
		log.Errorf("Revert failed: %s", err)

		statusMessage := umprotocol.StatusRsp{
			Status:           umprotocol.FailedStatus,
			Operation:        umprotocol.RevertOperation,
			RequestedVersion: revertReq.ImageVersion,
			CurrentVersion:   processor.updater.GetCurrentVersion(),
			Error:            err.Error(),
		}

		return marshalResponse(umprotocol.StatusResponseType, &statusMessage)
	}

	return nil, nil
}

func marshalResponse(messageType string, data interface{}) (messageJSON []byte, err error) {
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
