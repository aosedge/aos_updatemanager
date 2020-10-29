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
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/umprotocol"
	"gitpct.epam.com/epmd-aepr/aos_common/wsserver"

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
	wsServer   *wsserver.Server
	updater    Updater
	crtHandler CrtHandler
}

// Updater interface
type Updater interface {
	GetStatus() (status []umprotocol.ComponentStatus)
	Update(infos []umprotocol.ComponentInfo)
	StatusChannel() (statusChannel <-chan []umprotocol.ComponentStatus)
}

// CrtHandler interface
type CrtHandler interface {
	CreateKeys(crtType, systemdID, password string) (csr string, err error)
	ApplyCertificate(crtType string, crt string) (crtURL string, err error)
	GetCertificate(crtType string, issuer []byte, serial string) (crtURL, keyURL string, err error)
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new Web socket server
func New(cfg *config.Config, updater Updater, crtHandler CrtHandler) (server *Server, err error) {
	server = &Server{updater: updater, crtHandler: crtHandler}

	if server.wsServer, err = wsserver.New("UM", cfg.ServerURL, cfg.Cert, cfg.Key, server); err != nil {
		return nil, err
	}

	go func() {
		for {
			status, ok := <-updater.StatusChannel()
			if !ok {
				return
			}

			server.sendStatus(status)
		}
	}()

	return server, nil
}

// Close closes web socket server and all connections
func (server *Server) Close() {
	server.wsServer.Close()
}

// ClientConnected called when new client is connected
func (server *Server) ClientConnected(client *wsserver.Client) {
}

// ClientDisconnected called when client is disconnected
func (server *Server) ClientDisconnected(client *wsserver.Client) {
}

// ProcessMessage called when new message is received
func (server *Server) ProcessMessage(client *wsserver.Client, messageType int, messageJSON []byte) (response []byte, err error) {
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
	case umprotocol.GetComponentsRequestType:
		return server.processGetComponents()

	case umprotocol.UpdateRequestType:
		return server.processUpdate(message.Data)

	case umprotocol.CreateKeysRequestType:
		return server.processCreateKeys(message.Data)

	case umprotocol.ApplyCertRequestType:
		return server.processApplyCert(message.Data)

	case umprotocol.GetCertRequestType:
		return server.processGetCert(message.Data)

	default:
		return nil, errors.New("unsupported request type: " + message.Header.MessageType)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *Server) sendStatus(status []umprotocol.ComponentStatus) {
	log.Debug("Send components status")

	statusJSON, err := marshalResponse(umprotocol.UpdateStatusType, status)
	if err != nil {
		log.Errorf("Can't marshal status message: %s", err)
	}

	for _, client := range server.wsServer.GetClients() {
		if err = client.SendMessage(websocket.TextMessage, statusJSON); err != nil {
			log.Errorf("Can't send status message: %s", err)
		}
	}
}

func (server *Server) processGetComponents() (response []byte, err error) {
	status := server.updater.GetStatus()

	log.Debug("Process get components request")

	return marshalResponse(umprotocol.GetComponentsResponseType, &status)
}

func (server *Server) processUpdate(request []byte) (response []byte, err error) {
	var infos []umprotocol.ComponentInfo

	if err = json.Unmarshal(request, &infos); err != nil {
		return nil, err
	}

	log.Debug("Process update request")

	server.updater.Update(infos)

	return nil, nil
}

func (server *Server) processCreateKeys(request []byte) (response []byte, err error) {
	var createKeysReq umprotocol.CreateKeysReq

	if err = json.Unmarshal(request, &createKeysReq); err != nil {
		return nil, err
	}

	log.WithField("type", createKeysReq.Type).Debug("Process create keys request")

	createKeysRsp := umprotocol.CreateKeysRsp{Type: createKeysReq.Type}

	if createKeysRsp.Csr, err = server.crtHandler.CreateKeys(createKeysReq.Type,
		createKeysReq.SystemID, createKeysReq.Password); err != nil {
		createKeysRsp.Error = err.Error()
	}

	return marshalResponse(umprotocol.CreateKeysResponseType, &createKeysRsp)
}

func (server *Server) processApplyCert(request []byte) (response []byte, err error) {
	var applyCertReq umprotocol.ApplyCertReq

	if err = json.Unmarshal(request, &applyCertReq); err != nil {
		return nil, err
	}

	log.WithField("type", applyCertReq.Type).Debug("Process apply cert request")

	applyCertRsp := umprotocol.ApplyCertRsp{Type: applyCertReq.Type}

	if applyCertRsp.CrtURL, err = server.crtHandler.ApplyCertificate(applyCertReq.Type, applyCertReq.Crt); err != nil {
		applyCertRsp.Error = err.Error()
	}

	return marshalResponse(umprotocol.ApplyCertResponseType, &applyCertRsp)
}

func (server *Server) processGetCert(request []byte) (response []byte, err error) {
	var getCertReq umprotocol.GetCertReq

	if err = json.Unmarshal(request, &getCertReq); err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"type":   getCertReq.Type,
		"serial": getCertReq.Serial,
		"issuer": base64.StdEncoding.EncodeToString(getCertReq.Issuer)}).Debug("Process get cert request")

	getCertRsp := umprotocol.GetCertRsp{Type: getCertReq.Type}

	if getCertRsp.CrtURL, getCertRsp.KeyURL, err = server.crtHandler.GetCertificate(
		getCertReq.Type, getCertReq.Issuer, getCertReq.Serial); err != nil {
		getCertRsp.Error = err.Error()
	}

	return marshalResponse(umprotocol.GetCertResponseType, &getCertRsp)
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
