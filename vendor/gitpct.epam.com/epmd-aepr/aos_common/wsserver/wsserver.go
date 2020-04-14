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
	clients map[string]*Client
	handler ClientHandler
}

// Client websocket client handler
type Client struct {
	RemoteAddr string
	handler    ClientHandler
	connection *websocket.Conn
	sync.Mutex
}

// ClientHandler provides interface to handle client
type ClientHandler interface {
	ClientConnected(client *Client)
	ProcessMessage(client *Client, messageType int, message []byte) (response []byte, err error)
	ClientDisconnected(client *Client)
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new Web socket server
func New(name, url, cert, key string, handler ClientHandler) (server *Server, err error) {
	server = &Server{
		name: name,
		upgrader: websocket.Upgrader{
			// TODO: Implement proper check origin validation
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		handler: handler,
		clients: make(map[string]*Client),
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
func (server *Server) GetClients() (clients []*Client) {
	server.Lock()
	defer server.Unlock()

	clients = make([]*Client, 0, len(server.clients))

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
func (client *Client) SendMessage(messageType int, data []byte) (err error) {
	client.Lock()
	defer client.Unlock()

	if messageType == websocket.TextMessage {
		log.WithFields(log.Fields{
			"message":    string(data),
			"remoteAddr": client.RemoteAddr}).Debug("Send message")
	} else {
		log.WithFields(log.Fields{
			"message":    data,
			"remoteAddr": client.RemoteAddr}).Debug("Send message")
	}

	if writeSocketTimeout != 0 {
		client.connection.SetWriteDeadline(time.Now().Add(writeSocketTimeout))
	}

	if err = client.connection.WriteMessage(messageType, data); err != nil {
		if err != websocket.ErrCloseSent {
			log.Errorf("Can't write message: %s", err)
			client.connection.Close()
		}

		return err
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (server *Server) newClient(w http.ResponseWriter, r *http.Request) (client *Client, err error) {
	server.Lock()
	defer server.Unlock()

	defer func() {
		if err != nil {
			if client.connection != nil {
				client.connection.Close()
			}
		}
	}()

	client = &Client{RemoteAddr: r.RemoteAddr, handler: server.handler}

	if websocket.IsWebSocketUpgrade(r) != true {
		return nil, errors.New("new connection is not websocket")
	}

	if client.connection, err = server.upgrader.Upgrade(w, r, nil); err != nil {
		return nil, err
	}

	server.clients[client.RemoteAddr] = client

	return client, nil
}

func (server *Server) deleteClient(client *Client) (err error) {
	server.Lock()
	defer server.Unlock()

	delete(server.clients, client.RemoteAddr)
	client.close(false)

	return nil
}

func (client *Client) close(sendCloseMessage bool) (err error) {
	log.WithFields(log.Fields{
		"remoteAddr": client.connection.RemoteAddr()}).Info("Close client")

	if sendCloseMessage {
		client.SendMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}

	return client.connection.Close()
}

func (client *Client) run() {
	for {
		messageType, message, err := client.connection.ReadMessage()
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
				"removeAddr": client.RemoteAddr}).Debug("Receive message")
		} else {
			log.WithFields(log.Fields{
				"message":    message,
				"remoteAddr": client.RemoteAddr}).Debug("Receive message")
		}

		if client.handler != nil {
			response, err := client.handler.ProcessMessage(client, messageType, message)
			if err != nil {
				log.Errorf("Can't process message: %s", err)
				continue
			}

			if response != nil {
				if err := client.SendMessage(messageType, response); err != nil {
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

	client, err := server.newClient(w, r)
	if err != nil {
		log.Errorf("Can't create client handler: %s", err)
		return
	}

	if server.handler != nil {
		server.handler.ClientConnected(client)
	}

	client.run()

	if err = server.deleteClient(client); err != nil {
		log.Errorf("Can't delete client handler: %s", err)
	}

	if server.handler != nil {
		server.handler.ClientDisconnected(client)
	}
}
