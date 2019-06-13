package wsserver

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	writeSocketTimeout = 10 * time.Second
)

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

/*******************************************************************************
 * Types
 ******************************************************************************/

type wsClient struct {
	connection  *websocket.Conn
	remoteClose bool
	sync.Mutex
}

// MessageHeader UM message header
type MessageHeader struct {
	Type  string `json:"type"`
	Error string `json:"error,omitempty"`
}

// UpgradeReq system upgrade request
type UpgradeReq struct {
	MessageHeader
	ImageVersion uint64 `json:"imageVersion"`
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

/*******************************************************************************
 * Variables
 ******************************************************************************/

/*******************************************************************************
 * Private
 ******************************************************************************/

func newClient(connection *websocket.Conn) (client *wsClient, err error) {
	log.WithField("RemoteAddr", connection.RemoteAddr()).Info("Create new client")

	client = &wsClient{
		connection: connection}

	return client, nil
}

func (client *wsClient) close() (err error) {
	log.WithField("RemoteAddr", client.connection.RemoteAddr()).Info("Close client")

	if !client.remoteClose {
		client.sendMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}

	return client.connection.Close()
}

func (client *wsClient) run() {
	for {
		messageType, message, err := client.connection.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) &&
				!strings.Contains(err.Error(), "use of closed network connection") {
				log.Errorf("Error reading socket: %s", err)
			}

			client.remoteClose = true

			break
		}

		log.Debugf("Receive: %s", string(message))

		var header MessageHeader
		var response []byte

		if err = json.Unmarshal(message, &header); err != nil {
			response = createResponseError(MessageHeader{"", ""}, err)
		} else {
			switch messageType {
			case websocket.TextMessage:
				response, err = client.processIncomingMessage(header, message)

			default:
				log.WithField("format", messageType).Warning("Incoming message in unsupported format")
				response = createResponseError(header, err)
			}

			if err != nil {
				response = createResponseError(header, err)
			}
		}

		client.sendMessage(websocket.TextMessage, response)
	}
}

func (client *wsClient) processIncomingMessage(header MessageHeader, requestJSON []byte) (responseJSON []byte, err error) {
	switch string(header.Type) {
	case StatusType:
		return client.processGetStatus()

	case UpgradeType:
		return client.processSystemUpgrade(requestJSON)

	case RevertType:
		return client.processSystemRevert(requestJSON)

	default:
		return nil, errors.New("unsupported request type: " + header.Type)
	}
}

func (client *wsClient) processGetStatus() (responseJSON []byte, err error) {
	response := StatusMessage{
		MessageHeader:    MessageHeader{Type: StatusType},
		Operation:        UpgradeType,
		Status:           SuccessStatus,
		OperationVersion: 1,
		ImageVersion:     0}

	log.Debug("Process get status request")

	if responseJSON, err = json.Marshal(response); err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func (client *wsClient) processSystemUpgrade(requestJSON []byte) (responseJSON []byte, err error) {
	var upgradeReq UpgradeReq

	if err = json.Unmarshal(requestJSON, &upgradeReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", upgradeReq.ImageVersion).Debug("Process system upgrade request")

	response := StatusMessage{
		MessageHeader:    MessageHeader{Type: StatusType},
		Operation:        UpgradeType,
		Status:           SuccessStatus,
		OperationVersion: 1,
		ImageVersion:     0}

	if responseJSON, err = json.Marshal(response); err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func (client *wsClient) processSystemRevert(requestJSON []byte) (responseJSON []byte, err error) {
	var revertReq RevertReq

	if err = json.Unmarshal(requestJSON, &revertReq); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", revertReq.ImageVersion).Debug("Process system revert request")

	response := StatusMessage{
		MessageHeader:    MessageHeader{Type: StatusType},
		Operation:        RevertType,
		Status:           SuccessStatus,
		OperationVersion: 1,
		ImageVersion:     0}

	if responseJSON, err = json.Marshal(response); err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func createResponseError(header MessageHeader, err error) (responseJSON []byte) {
	header.Error = err.Error()

	if responseJSON, err = json.Marshal(header); err != nil {
		return []byte(err.Error())
	}

	return responseJSON
}

func (client *wsClient) sendMessage(messageType int, data []byte) (err error) {
	client.Lock()
	defer client.Unlock()

	if messageType == websocket.TextMessage {
		log.Debugf("Send: %s", string(data))
	} else {
		log.Debugf("Send: %v", data)
	}

	if writeSocketTimeout != 0 {
		client.connection.SetWriteDeadline(time.Now().Add(writeSocketTimeout))
	}

	if err = client.connection.WriteMessage(messageType, data); err != nil {
		log.Errorf("Can't write message: %s", err)

		client.connection.Close()

		return err
	}

	return nil
}
