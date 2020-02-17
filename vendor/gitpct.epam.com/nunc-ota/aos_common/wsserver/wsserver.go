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

package wsserver

import (
	"context"
	"errors"
	"net/http"
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

/*******************************************************************************
 * Types
 ******************************************************************************/

// Server websocket server structure
type Server struct {
	name       string
	httpServer *http.Server
	upgrader   websocket.Upgrader
	sync.Mutex
	clients        map[string]*ClientHandler
	processMessage ProcessMessage
}

// ClientHandler websocket client handler
type ClientHandler struct {
	RemoteAddr     string
	processMessage ProcessMessage
	connection     *websocket.Conn
	sync.Mutex
}

// ProcessMessage callback function used to process incoming messagess
type ProcessMessage func(messageType int, message []byte) (response []byte, err error)

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new Web socket server
func New(name, url, cert, key string, processMessage ProcessMessage) (server *Server, err error) {
	server = &Server{
		name: name,
		upgrader: websocket.Upgrader{
			// TODO: Implement proper check origin validation
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		processMessage: processMessage,
		clients:        make(map[string]*ClientHandler),
	}

	log.WithField("server", server.name).Debug("Create ws server")

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", server.handleConnection)

	server.httpServer = &http.Server{Addr: url, Handler: serveMux}

	go func(crt, key string) {
		log.WithFields(log.Fields{"address": url, "crt": crt, "key": key}).Debug("Listen for clients")

		if err := server.httpServer.ListenAndServeTLS(crt, key); err != http.ErrServerClosed {
			log.Error("Server listening error: ", err)
			return
		}
	}(cert, key)

	return server, nil
}

// GetClients return client list
func (server *Server) GetClients() (clients []*ClientHandler) {
	server.Lock()
	defer server.Unlock()

	clients = make([]*ClientHandler, 0, len(server.clients))

	for _, client := range server.clients {
		clients = append(clients, client)
	}

	return clients
}

// Close closes web socket server and all connections
func (server *Server) Close() {
	server.Lock()
	defer server.Unlock()

	log.WithField("server", server.name).Debug("Close ws server")

	for _, client := range server.clients {
		client.close(true)
	}

	server.httpServer.Shutdown(context.Background())
}

// SendMessage sends message to ws client
func (handler *ClientHandler) SendMessage(messageType int, data []byte) (err error) {
	handler.Lock()
	defer handler.Unlock()

	if messageType == websocket.TextMessage {
		log.WithFields(log.Fields{
			"message":    string(data),
			"remoteAddr": handler.RemoteAddr}).Debug("Send message")
	} else {
		log.WithFields(log.Fields{
			"message":    data,
			"remoteAddr": handler.RemoteAddr}).Debug("Send message")
	}

	if writeSocketTimeout != 0 {
		handler.connection.SetWriteDeadline(time.Now().Add(writeSocketTimeout))
	}

	if err = handler.connection.WriteMessage(messageType, data); err != nil {
		if err != websocket.ErrCloseSent {
			log.Errorf("Can't write message: %s", err)
			handler.connection.Close()
		}

		return err
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *Server) newHandler(w http.ResponseWriter, r *http.Request) (handler *ClientHandler, err error) {
	server.Lock()
	defer server.Unlock()

	defer func() {
		if err != nil {
			if handler.connection != nil {
				handler.connection.Close()
			}
		}
	}()

	handler = &ClientHandler{RemoteAddr: r.RemoteAddr, processMessage: server.processMessage}

	if websocket.IsWebSocketUpgrade(r) != true {
		return nil, errors.New("new connection is not websocket")
	}

	if handler.connection, err = server.upgrader.Upgrade(w, r, nil); err != nil {
		return nil, err
	}

	server.clients[handler.RemoteAddr] = handler

	return handler, nil
}

func (server *Server) deleteHandler(handler *ClientHandler) (err error) {
	server.Lock()
	defer server.Unlock()

	delete(server.clients, handler.RemoteAddr)
	handler.close(false)

	return nil
}

func (handler *ClientHandler) close(sendCloseMessage bool) (err error) {
	log.WithFields(log.Fields{
		"remoteAddr": handler.connection.RemoteAddr()}).Info("Close client")

	if sendCloseMessage {
		handler.SendMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}

	return handler.connection.Close()
}

func (handler *ClientHandler) run() {
	for {
		messageType, message, err := handler.connection.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) &&
				!strings.Contains(err.Error(), "use of closed network connection") {
				log.Errorf("Error reading socket: %s", err)
			}

			break
		}

		if messageType == websocket.TextMessage {
			log.WithFields(log.Fields{
				"message":    string(message),
				"removeAddr": handler.RemoteAddr}).Debug("Receive message")
		} else {
			log.WithFields(log.Fields{
				"message":    message,
				"remoteAddr": handler.RemoteAddr}).Debug("Receive message")
		}

		if handler.processMessage != nil {
			response, err := handler.processMessage(messageType, message)
			if err != nil {
				log.Errorf("Can't process message: %s", err)
				continue
			}

			if response != nil {
				if err := handler.SendMessage(messageType, response); err != nil {
					log.Errorf("Can't send message: %s", err)
				}
			}
		}
	}
}

func (server *Server) handleConnection(w http.ResponseWriter, r *http.Request) {
	log.WithFields(log.Fields{
		"remoteAddr": r.RemoteAddr,
		"server":     server.name}).Debug("New connection request")

	handler, err := server.newHandler(w, r)
	if err != nil {
		log.Errorf("Can't create client handler: %s", err)
		return
	}

	handler.run()

	if err = server.deleteHandler(handler); err != nil {
		log.Errorf("Can't delete client handler: %s", err)
	}
}
