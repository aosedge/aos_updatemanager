package umserver

import (
	"encoding/json"
	"errors"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_servicemanager/wsserver"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

// Message types
const (
	RevertType  = "systemRevert"
	StatusType  = "status"
	UpgradeType = "systemUpgrade"
)

// Operation status
const (
	SuccessStatus    = "success"
	FailedStatus     = "failed"
	InProgressStatus = "inprogress"
)

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

// MessageHeader UM message header
type MessageHeader struct {
	Type  string `json:"type"`
	Error string `json:"error,omitempty"`
}

// UpgradeFileInfo upgrade file info
type UpgradeFileInfo struct {
	Target string `json:"target"`
	URL    string `json:"url"`
	Sha256 []byte `json:"sha256"`
	Sha512 []byte `json:"sha512"`
	Size   uint64 `json:"size"`
}

// UpgradeReq system upgrade request
type UpgradeReq struct {
	MessageHeader
	ImageVersion uint64            `json:"imageVersion"`
	Files        []UpgradeFileInfo `json:"files"`
}

// RevertReq system revert request
type RevertReq struct {
	MessageHeader
	ImageVersion uint64 `json:"imageVersion"`
}

// GetStatusReq get system status request
type GetStatusReq struct {
	MessageHeader
}

// StatusMessage status message
type StatusMessage struct {
	MessageHeader
	Operation        string `json:"operation"` // upgrade, revert
	Status           string `json:"status"`    // success, failed, inprogress
	OperationVersion uint64 `json:"operationVersion"`
	ImageVersion     uint64 `json:"imageVersion"`
}

// Updater interface
type Updater interface {
	GetVersion() (version uint64)
	GetOperationVersion() (version uint64)
	GetState() (state int)
	GetLastError() (err error)
	Upgrade(version uint64, filesInfo []UpgradeFileInfo) (err error)
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
	var header MessageHeader

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

func (processor *messageProcessor) processIncomingMessage(header MessageHeader, request []byte) (response []byte, err error) {
	switch string(header.Type) {
	case StatusType:
		return processor.processGetStatus()

	case UpgradeType:
		return processor.processSystemUpgrade(request)

	case RevertType:
		return processor.processSystemRevert(request)

	default:
		return nil, errors.New("unsupported request type: " + header.Type)
	}
}

func (processor *messageProcessor) processGetStatus() (response []byte, err error) {
	state := processor.updater.GetState()
	status := SuccessStatus
	errStr := ""
	operation := ""

	if state == RevertingState || state == RevertedState {
		operation = RevertType
	}

	if state == UpgradingState || state == UpgradedState {
		operation = UpgradeType
	}

	if state == RevertingState || state == UpgradingState {
		status = InProgressStatus
	}

	if err = processor.updater.GetLastError(); err != nil {
		status = FailedStatus
		errStr = err.Error()
	}

	statusMessage := StatusMessage{
		MessageHeader: MessageHeader{
			Type:  StatusType,
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
	var upgradeReq UpgradeReq

	if err = json.Unmarshal(request, &upgradeReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", upgradeReq.ImageVersion).Debug("Process system upgrade request")

	go func() {
		statusMessage := StatusMessage{
			MessageHeader:    MessageHeader{Type: StatusType},
			Status:           SuccessStatus,
			Operation:        UpgradeType,
			OperationVersion: upgradeReq.ImageVersion}

		if err := processor.updater.Upgrade(upgradeReq.ImageVersion, upgradeReq.Files); err != nil {
			log.Errorf("Upgrade failed: %s", err)

			statusMessage.Error = err.Error()
			statusMessage.Status = FailedStatus
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
	var revertReq RevertReq

	if err = json.Unmarshal(request, &revertReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", revertReq.ImageVersion).Debug("Process system revert request")

	go func() {
		statusMessage := StatusMessage{
			MessageHeader:    MessageHeader{Type: StatusType},
			Status:           SuccessStatus,
			Operation:        RevertType,
			OperationVersion: revertReq.ImageVersion}

		if err := processor.updater.Revert(revertReq.ImageVersion); err != nil {
			log.Errorf("Revert failed: %s", err)

			statusMessage.Error = err.Error()
			statusMessage.Status = FailedStatus
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

func createResponseError(header MessageHeader, responseErr error) (response []byte, err error) {
	header.Error = responseErr.Error()

	if response, err = json.Marshal(header); err != nil {
		return nil, err
	}

	return response, err
}
