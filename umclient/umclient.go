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
	pb "github.com/aosedge/aos_common/api/updatemanager/v1"
	"github.com/aosedge/aos_common/utils/cryptutils"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
}

// UMState UM state.
type UMState int32

// ComponentStatus component status.
type ComponentStatus int32

// ComponentUpdateInfo component update info.
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

// ComponentStatusInfo component status info.
type ComponentStatusInfo struct {
	ID            string
	VendorVersion string
	AosVersion    uint64
	Status        ComponentStatus
	Error         string
}

// Status update manager status.
type Status struct {
	State      UMState
	Error      string
	Components []ComponentStatusInfo
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
	GetCertificate(certType string) (certURL, ketURL string, err error)
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

	go func() {
		for {
			select {
			case <-client.closeChannel:
				return

			case status := <-client.messageHandler.StatusChannel():
				if err := client.sendStatus(status); err != nil {
					log.Errorf("Can't send status: %s", aoserrors.Wrap(err))
				}
			}
		}
	}()

	return client, nil
}

// Close closes UM client.
func (client *Client) Close() (err error) {
	log.Debug("Close UM client")

	if client.stream != nil {
		err = aoserrors.Wrap(client.stream.CloseSend())
	}

	if client.connection != nil {
		client.connection.Close()
	}

	close(client.closeChannel)

	return aoserrors.Wrap(err)
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
	log.Debug("Connecting to CM...")

	var secureOpt grpc.DialOption

	if insecureConn {
		secureOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		certURL, keyURL, err := provider.GetCertificate(config.CertStorage)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		tlsConfig, err := cryptocontext.GetClientMutualTLSConfig(certURL, keyURL)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		secureOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	if client.connection, err = grpc.DialContext(ctx, config.CMServerURL, secureOpt, grpc.WithBlock()); err != nil {
		return aoserrors.Wrap(err)
	}

	log.Debug("Connected to CM")

	go func() {
		err := client.register()

		for {
			if err != nil && len(client.closeChannel) == 0 {
				log.Errorf("Error register to CM: %s", aoserrors.Wrap(err))
			} else {
				client.messageHandler.Registered()

				if err = client.processMessages(); err != nil {
					if errors.Is(err, io.EOF) {
						log.Debug("Connection is closed")
					} else {
						log.Errorf("Connection error: %s", aoserrors.Wrap(err))
					}
				}
			}

			log.Debugf("Reconnect to CM in %v...", reconnectTimeout)

			select {
			case <-client.closeChannel:
				log.Debugf("Disconnected from CM")

				return

			case <-time.After(reconnectTimeout):
				err = client.register()
			}
		}
	}()

	return nil
}

func (client *Client) register() (err error) {
	client.Lock()
	defer client.Unlock()

	log.Debug("Registering to CM...")

	if client.stream, err = pb.NewUMServiceClient(client.connection).RegisterUM(context.Background()); err != nil {
		return aoserrors.Wrap(err)
	}

	log.Debug("Registered to CM")

	return nil
}

func (client *Client) processMessages() (err error) {
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
						ID:            component.GetId(),
						VendorVersion: component.GetVendorVersion(),
						AosVersion:    component.GetAosVersion(),
						Annotations:   json.RawMessage(component.GetAnnotations()),
						URL:           component.GetUrl(),
						Sha256:        component.GetSha256(),
						Sha512:        component.GetSha512(),
						Size:          component.GetSize(),
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

func (client *Client) sendStatus(status Status) (err error) {
	client.Lock()
	defer client.Unlock()

	if client.stream == nil {
		return aoserrors.New("client is not connected")
	}

	log.WithFields(log.Fields{"umID": client, "state": status.State, "error": status.Error}).Debug("Send status")

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
		return aoserrors.Wrap(err)
	}

	return nil
}
