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

// State
const (
	UpgradedState = iota
	UpgradingState
	RevertedState
	RevertingState
)

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
	GetVersion() (version uint64)
	GetOperationVersion() (version uint64)
	GetState() (state int)
	GetLastError() (err error)
	Upgrade(version uint64, filesInfo []umprotocol.UpgradeFileInfo) (err error)
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
func (processor *messageProcessor) ProcessMessage(messageType int, message []byte) (response []byte, err error) {
	var header umprotocol.MessageHeader

	if messageType != websocket.TextMessage {
		return nil, errors.New("incoming message in unsupported format")
	}

	if err = json.Unmarshal(message, &header); err != nil {
		return nil, err
	}

	if response, err = processor.processIncomingMessage(header, message); err != nil {
		return createResponseError(header, err)
	}

	return response, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *Server) newMessageProcessor(sendMessage wsserver.SendMessage) (processor wsserver.MessageProcessor, err error) {
	return &messageProcessor{updater: server.updater, sendMessage: sendMessage}, nil
}

func (processor *messageProcessor) processIncomingMessage(header umprotocol.MessageHeader, request []byte) (response []byte, err error) {
	switch string(header.Type) {
	case umprotocol.StatusType:
		return processor.processGetStatus()

	case umprotocol.UpgradeType:
		return processor.processSystemUpgrade(request)

	case umprotocol.RevertType:
		return processor.processSystemRevert(request)

	default:
		return nil, errors.New("unsupported request type: " + header.Type)
	}
}

func (processor *messageProcessor) processGetStatus() (response []byte, err error) {
	state := processor.updater.GetState()
	status := umprotocol.SuccessStatus
	errStr := ""
	operation := ""

	if state == RevertingState || state == RevertedState {
		operation = umprotocol.RevertType
	}

	if state == UpgradingState || state == UpgradedState {
		operation = umprotocol.UpgradeType
	}

	if state == RevertingState || state == UpgradingState {
		status = umprotocol.InProgressStatus
	}

	if err = processor.updater.GetLastError(); err != nil {
		status = umprotocol.FailedStatus
		errStr = err.Error()
	}

	statusMessage := umprotocol.StatusMessage{
		MessageHeader: umprotocol.MessageHeader{
			Type:  umprotocol.StatusType,
			Error: errStr},
		Operation:        operation,
		Status:           status,
		OperationVersion: processor.updater.GetOperationVersion(),
		ImageVersion:     processor.updater.GetVersion()}

	log.Debug("Process get status request")

	if response, err = json.Marshal(statusMessage); err != nil {
		return nil, err
	}

	return response, nil
}

func (processor *messageProcessor) processSystemUpgrade(request []byte) (response []byte, err error) {
	var upgradeReq umprotocol.UpgradeReq

	if err = json.Unmarshal(request, &upgradeReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", upgradeReq.ImageVersion).Debug("Process system upgrade request")

	go func() {
		statusMessage := umprotocol.StatusMessage{
			MessageHeader:    umprotocol.MessageHeader{Type: umprotocol.StatusType},
			Status:           umprotocol.SuccessStatus,
			Operation:        umprotocol.UpgradeType,
			OperationVersion: upgradeReq.ImageVersion}

		if err := processor.updater.Upgrade(upgradeReq.ImageVersion, upgradeReq.Files); err != nil {
			log.Errorf("Upgrade failed: %s", err)

			statusMessage.Error = err.Error()
			statusMessage.Status = umprotocol.FailedStatus
		}

		statusMessage.ImageVersion = processor.updater.GetVersion()

		statusJSON, err := json.Marshal(statusMessage)
		if err != nil {
			log.Errorf("Can't marshal status message: %s", err)
		}

		if err = processor.sendMessage(websocket.TextMessage, statusJSON); err != nil {
			log.Errorf("Can't send status message: %s", err)
		}
	}()

	return nil, nil
}

func (processor *messageProcessor) processSystemRevert(request []byte) (response []byte, err error) {
	var revertReq umprotocol.RevertReq

	if err = json.Unmarshal(request, &revertReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", revertReq.ImageVersion).Debug("Process system revert request")

	go func() {
		statusMessage := umprotocol.StatusMessage{
			MessageHeader:    umprotocol.MessageHeader{Type: umprotocol.StatusType},
			Status:           umprotocol.SuccessStatus,
			Operation:        umprotocol.RevertType,
			OperationVersion: revertReq.ImageVersion}

		if err := processor.updater.Revert(revertReq.ImageVersion); err != nil {
			log.Errorf("Revert failed: %s", err)

			statusMessage.Error = err.Error()
			statusMessage.Status = umprotocol.FailedStatus
		}

		statusMessage.ImageVersion = processor.updater.GetVersion()

		statusJSON, err := json.Marshal(statusMessage)
		if err != nil {
			log.Errorf("Can't marshal status message: %s", err)
		}

		if err = processor.sendMessage(websocket.TextMessage, statusJSON); err != nil {
			log.Errorf("Can't send status message: %s", err)
		}
	}()

	return nil, nil
}

func createResponseError(header umprotocol.MessageHeader, responseErr error) (response []byte, err error) {
	header.Error = responseErr.Error()

	if response, err = json.Marshal(header); err != nil {
		return nil, err
	}

	return response, err
}
