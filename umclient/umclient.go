// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
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
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/api/common"
	"github.com/aosedge/aos_common/api/iamanager"
	pb "github.com/aosedge/aos_common/api/updatemanager"
	"github.com/aosedge/aos_common/utils/cryptutils"
	"github.com/aosedge/aos_common/utils/grpchelpers"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aosedge/aos_updatemanager/config"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	connectTimeout   = 30 * time.Second
	reconnectTimeout = 10 * time.Second
)

// UM states.
const (
	StateIdle = iota
	StatePrepared
	StateUpdated
	StateFailed
)

// Component statuses.
const (
	StatusInstalled = iota
	StatusInstalling
	StatusError
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Client UM client instance.
type Client struct {
	sync.Mutex
	connection     *grpc.ClientConn
	stream         pb.UMService_RegisterUMClient
	messageHandler MessageHandler
	umID           string
	closeChannel   chan struct{}
	certChannel    <-chan *iamanager.CertInfo
}

// UMState UM state.
type UMState int32

// ComponentStatus component status.
type ComponentStatus int32

// ComponentUpdateInfo component update info.
type ComponentUpdateInfo struct {
	ID          string
	Type        string
	Version     string
	Annotations json.RawMessage
	URL         string
	Sha256      []byte
	Size        uint64
}

// ComponentStatusInfo component status info.
type ComponentStatusInfo struct {
	ID      string
	Type    string
	Version string
	Status  ComponentStatus
	Error   string
}

// Status update manager status.
type Status struct {
	State      UMState
	Components []ComponentStatusInfo
	Error      string
}

// MessageHandler incoming messages handler.
type MessageHandler interface {
	// Registered indicates the client registered on the server
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

// CertificateProvider interface to get certificate.
type CertificateProvider interface {
	GetNodeID() (string, error)
	GetCertificate(certType string, issuer []byte, serial string) (certURL, keyURL string, err error)
	SubscribeCertChanged(certType string) (<-chan *iamanager.CertInfo, error)
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new UM client.
func New(cfg *config.Config, messageHandler MessageHandler, certProvider CertificateProvider,
	cryptocontext *cryptutils.CryptoContext, insecure bool,
) (client *Client, err error) {
	log.Debug("Create UM client")

	if messageHandler == nil {
		return nil, aoserrors.New("message handler is nil")
	}

	client = &Client{
		messageHandler: messageHandler,
		closeChannel:   make(chan struct{}),
	}

	if client.umID, err = certProvider.GetNodeID(); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = client.createConnection(cfg, certProvider, cryptocontext, insecure); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if !insecure {
		if client.certChannel, err = certProvider.SubscribeCertChanged(cfg.CertStorage); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	go client.processMessages()

	return client, nil
}

// Close closes UM client.
func (client *Client) Close() (err error) {
	close(client.closeChannel)

	client.closeGRPCConnection()

	return nil
}

func (state UMState) String() string {
	return [...]string{
		"idle", "prepared", "updated", "failed",
	}[state]
}

func (status ComponentStatus) String() string {
	return [...]string{
		"installed", "installing", "error",
	}[status]
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (client *Client) createConnection(
	config *config.Config, provider CertificateProvider,
	cryptocontext *cryptutils.CryptoContext, insecureConn bool,
) (err error) {
	if err := client.register(config, provider, cryptocontext, insecureConn); err != nil {
		return err
	}

	go func() {
		for {
			client.messageHandler.Registered()

			if err = client.processGRPCMessages(); err != nil {
				if errors.Is(err, io.EOF) {
					log.Debug("Connection is closed")
				} else {
					log.Errorf("Connection error: %v", aoserrors.Wrap(err))
				}
			}

			log.Debugf("Reconnect to CM in %v...", reconnectTimeout)

			client.closeGRPCConnection()

		reconnectionLoop:
			for {
				select {
				case <-client.closeChannel:
					log.Debug("Disconnected from CM")

					return

				case <-time.After(reconnectTimeout):
					if err := client.register(config, provider, cryptocontext, insecureConn); err != nil {
						log.WithField("err", err).Debug("Reconnection failed")
					} else {
						break reconnectionLoop
					}
				}
			}
		}
	}()

	return nil
}

func (client *Client) closeGRPCConnection() {
	client.Lock()
	defer client.Unlock()

	log.Debug("Closing CM connection...")

	if client.stream != nil {
		if err := client.stream.CloseSend(); err != nil {
			log.WithField("err", err).Error("UM client failed send close")
		}
	}

	if client.connection != nil {
		client.connection.Close()
		client.connection = nil
	}
}

func (client *Client) register(config *config.Config, provider CertificateProvider,
	cryptocontext *cryptutils.CryptoContext, insecureConn bool,
) (err error) {
	client.Lock()
	defer client.Unlock()

	log.Debug("Connecting to CM...")

	client.connection, err = grpchelpers.CreateProtectedConnection(config.CertStorage, config.CMServerURL,
		connectTimeout, cryptocontext, provider, insecureConn)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	log.Debug("Connected to CM")

	log.Debug("Registering to CM...")

	if client.stream, err = pb.NewUMServiceClient(client.connection).RegisterUM(context.Background()); err != nil {
		return aoserrors.Wrap(err)
	}

	log.Debug("Registered to CM")

	return nil
}

func (client *Client) processGRPCMessages() (err error) {
	for {
		message, err := client.stream.Recv()
		if err != nil {
			if code, ok := status.FromError(err); ok {
				if code.Code() == codes.Canceled {
					log.Debug("UM client connection closed")
					return nil
				}
			}

			return aoserrors.Wrap(err)
		}

		switch data := message.GetCMMessage().(type) {
		case *pb.CMMessages_PrepareUpdate:
			log.Debug("Prepare update received")

			components := make([]ComponentUpdateInfo, 0, len(data.PrepareUpdate.GetComponents()))

			for _, component := range data.PrepareUpdate.GetComponents() {
				components = append(components,
					ComponentUpdateInfo{
						ID:          component.GetComponentId(),
						Type:        component.GetComponentType(),
						Version:     component.GetVersion(),
						Annotations: json.RawMessage(component.GetAnnotations()),
						URL:         component.GetUrl(),
						Sha256:      component.GetSha256(),
						Size:        component.GetSize(),
					})
			}

			client.messageHandler.PrepareUpdate(components)

		case *pb.CMMessages_StartUpdate:
			log.Debug("Start update received")

			client.messageHandler.StartUpdate()

		case *pb.CMMessages_ApplyUpdate:
			log.Debug("Apply update received")

			client.messageHandler.ApplyUpdate()

		case *pb.CMMessages_RevertUpdate:
			log.Debug("Revert update received")

			client.messageHandler.RevertUpdate()
		}
	}
}

func (client *Client) processMessages() {
	for {
		select {
		case <-client.closeChannel:
			return

		case <-client.certChannel:
			log.Debug("TLS certificate changed")

			client.closeGRPCConnection()

		case status := <-client.messageHandler.StatusChannel():
			if err := client.sendStatus(status); err != nil {
				log.Errorf("Can't send status: %v", aoserrors.Wrap(err))
			}
		}
	}
}

func (client *Client) sendStatus(status Status) (err error) {
	client.Lock()
	defer client.Unlock()

	if client.stream == nil {
		return aoserrors.New("client is not connected")
	}

	log.WithFields(log.Fields{"umID": client, "state": status.State, "error": status.Error}).Debug("Send status")

	pbComponents := make([]*pb.ComponentStatus, 0, len(status.Components))

	for _, component := range status.Components {
		pbComponent := pb.ComponentStatus{
			ComponentId:   component.ID,
			ComponentType: component.Type,
			Version:       component.Version,
			State:         pb.ComponentState(component.Status),
		}

		if component.Error != "" {
			pbComponent.Error = &common.ErrorInfo{Message: component.Error}
		}

		pbComponents = append(pbComponents, &pbComponent)
	}

	message := pb.UpdateStatus{
		NodeId:      client.umID,
		UpdateState: pb.UpdateState(status.State),
		Components:  pbComponents,
	}

	if status.Error != "" {
		message.Error = &common.ErrorInfo{Message: status.Error}
	}

	if err = client.stream.Send(&message); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
