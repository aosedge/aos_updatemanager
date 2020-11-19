// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2020 Renesas Inc.
// Copyright 2020 EPAM Systems Inc.
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

package umclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	pb "gitpct.epam.com/epmd-aepr/aos_common/api/updatemanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	connectTimeout = 10 * time.Second
)

// UM states
const (
	StateIdle = iota
	StatePrepared
	StateUpdated
	StateFailed
)

// Component statuses
const (
	StatusInstalled = iota
	StatusInstalling
	StatusError
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Client UM client instance
type Client struct {
	sync.Mutex

	connection     *grpc.ClientConn
	stream         pb.UpdateController_RegisterUMClient
	messageHandler MessageHandler
	umID           string
	cancelContext  context.CancelFunc
	closeWG        *sync.WaitGroup
	closeChannel   chan struct{}
}

// UMState UM state
type UMState int32

// ComponentStatus component status
type ComponentStatus int32

// ComponentUpdateInfo component update info
type ComponentUpdateInfo struct {
	ID            string
	VendorVersion string
	AosVersion    uint64
	Annotations   json.RawMessage
	URL           string
	Sha256        []byte
	Sha512        []byte
	Size          uint64
}

// ComponentStatusInfo component status info
type ComponentStatusInfo struct {
	ID            string
	VendorVersion string
	AosVersion    uint64
	Status        ComponentStatus
	Error         string
}

// Status update manager status
type Status struct {
	State      UMState
	Error      string
	Components []ComponentStatusInfo
}

// MessageHandler incoming messages handler
type MessageHandler interface {
	// Registered indicates the client registed to the server
	Registered()
	// PrepareUpdate prepares update
	PrepareUpdate(components []ComponentUpdateInfo)
	// StartUpdate starts update
	StartUpdate()
	// ApplyUpdate applies update
	ApplyUpdate()
	// RevertUpdate reverts update
	RevertUpdate()
	// StatusChannel returns status channel
	StatusChannel() (channel <-chan Status)
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new UM client
func New(config *config.Config, messageHandler MessageHandler, insecure bool) (client *Client, err error) {
	log.Debug("Create UM client")

	if messageHandler == nil {
		return nil, errors.New("message handler is nil")
	}

	client = &Client{
		messageHandler: messageHandler,
		umID:           config.ID,
		closeWG:        &sync.WaitGroup{},
		closeChannel:   make(chan struct{})}

	client.closeWG.Add(1)

	go func() {
		defer client.closeWG.Done()

		var err error

		for {
			select {
			case <-client.closeChannel:
				return

			default:
				if err != nil {
					if client.connection != nil {
						client.connection.Close()
					}

					if err == io.EOF {
						log.Debug("Connection is closed")
					} else {
						log.Errorf("Connection error: %s", err)
					}
				}

				if err = client.createConnection(config.ServerURL, insecure); err == nil {
					err = client.processMessages()
				}
			}
		}
	}()

	client.closeWG.Add(1)

	go func() {
		defer client.closeWG.Done()

		for {
			select {
			case <-client.closeChannel:
				return

			case status := <-client.messageHandler.StatusChannel():
				if err := client.sendStatus(status); err != nil {
					log.Errorf("Can't send status: %s", err)
				}
			}
		}
	}()

	return client, nil
}

// Close closes UM client
func (client *Client) Close() (err error) {
	client.Lock()

	log.Debug("Close UM client")

	if client.cancelContext != nil {
		client.cancelContext()
	}

	if client.stream != nil {
		client.stream.CloseSend()
	}

	if client.connection != nil {
		client.connection.Close()
	}

	client.Unlock()

	close(client.closeChannel)

	client.closeWG.Wait()

	return nil
}

func (state UMState) String() string {
	return [...]string{
		"idle", "prepared", "updated", "failed"}[state]
}

func (status ComponentStatus) String() string {
	return [...]string{
		"installed", "installing", "error"}[status]
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (client *Client) createConnection(url string, insecure bool) (err error) {
	client.Lock()

	log.Debug("Connecting to SM...")

	var secureOpt grpc.DialOption

	if insecure {
		secureOpt = grpc.WithInsecure()
	} else {
		secureOpt = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: false}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	client.cancelContext = cancel

	client.Unlock()

	connection, err := grpc.DialContext(ctx, url, secureOpt, grpc.WithBlock())
	if err != nil {
		return err
	}

	client.Lock()

	client.connection = connection

	log.Debug("Connected to SM")

	log.Debug("Registering to SM...")

	client.Unlock()

	stream, err := pb.NewUpdateControllerClient(client.connection).RegisterUM(context.Background())
	if err != nil {
		return err
	}

	log.Debug("Registered to SM")

	client.messageHandler.Registered()

	client.Lock()

	client.stream = stream

	client.Unlock()

	return nil
}

func (client *Client) processMessages() (err error) {
	for {
		message, err := client.stream.Recv()
		if err != nil {
			return err
		}

		switch data := message.SmMessage.(type) {
		case *pb.SmMessages_PrepareUpdate:
			log.Debug("Prepare update received")

			components := make([]ComponentUpdateInfo, 0, len(data.PrepareUpdate.Components))

			for _, component := range data.PrepareUpdate.Components {
				components = append(components,
					ComponentUpdateInfo{
						ID:            component.Id,
						VendorVersion: component.VendorVersion,
						AosVersion:    component.AosVersion,
						Annotations:   json.RawMessage(component.Annotations),
						URL:           component.Url,
						Sha256:        component.Sha256,
						Sha512:        component.Sha512,
						Size:          component.Size,
					})
			}

			client.messageHandler.PrepareUpdate(components)

		case *pb.SmMessages_StartUpdate:
			log.Debug("Start update received")

			client.messageHandler.StartUpdate()

		case *pb.SmMessages_ApplyUpdate:
			log.Debug("Apply update received")

			client.messageHandler.ApplyUpdate()

		case *pb.SmMessages_RevertUpdate:
			log.Debug("Revert update received")

			client.messageHandler.RevertUpdate()
		}
	}
}

func (client *Client) sendStatus(status Status) (err error) {
	client.Lock()
	defer client.Unlock()

	if client.stream == nil {
		return errors.New("client is not connected")
	}

	log.WithFields(log.Fields{"umID": client.umID, "state": status.State, "error": status.Error}).Debug("Send status")

	pbComponents := make([]*pb.SystemComponent, 0, len(status.Components))

	for _, component := range status.Components {
		pbComponents = append(pbComponents, &pb.SystemComponent{
			Id:            component.ID,
			VendorVersion: component.VendorVersion,
			AosVersion:    component.AosVersion,
			Status:        pb.ComponentStatus(component.Status),
			Error:         component.Error,
		})
	}

	if err = client.stream.Send(&pb.UpdateStatus{
		UmId:       client.umID,
		UmState:    pb.UmState(status.State),
		Error:      status.Error,
		Components: pbComponents,
	}); err != nil {
		return err
	}

	return nil
}
