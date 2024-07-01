// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2022 Renesas Electronics Corporation.
// Copyright (C) 2022 EPAM Systems, Inc.
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

package iamclient

import (
	"context"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	pb "github.com/aosedge/aos_common/api/iamanager/v4"
	"github.com/aosedge/aos_common/utils/cryptutils"
	"github.com/golang/protobuf/ptypes/empty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aosedge/aos_updatemanager/config"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const iamRequestTimeout = 30 * time.Second

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Client IAM client instance.
type Client struct {
	connection *grpc.ClientConn
	service    pb.IAMPublicServiceClient
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new IAM client.
func New(config *config.Config, cryptocontext *cryptutils.CryptoContext, insecure bool) (client *Client, err error) {
	client = &Client{}

	if client.connection, client.service, err = client.createConnection(
		cryptocontext, config.IAMPublicServerURL, insecure); err != nil {
		return client, err
	}

	defer func() {
		if err != nil {
			client.Close()
		}
	}()

	return client, nil
}

// Close closes IAM client.
func (client *Client) Close() error {
	if client.connection != nil {
		if err := client.connection.Close(); err != nil {
			return aoserrors.Wrap(err)
		}

		log.Debug("Disconnected from IAM")
	}

	return nil
}

// GetNodeID returns node ID.
func (client *Client) GetNodeID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.service.GetNodeInfo(ctx, &empty.Empty{})
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"nodeID": response.GetNodeId(), "nodeType": response.GetNodeType()}).Debug("Get node info")

	return response.GetNodeId(), nil
}

// GetCertificate gets certificate by issuer.
func (client *Client) GetCertificate(cerType string) (certURL, keyURL string, err error) {
	log.WithFields(log.Fields{"type": cerType}).Debug("Get certificate")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := client.service.GetCert(ctx, &pb.GetCertRequest{Type: cerType})
	if err != nil {
		return "", "", aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{
		"certURL": response.GetCertUrl(), "keyURL": response.GetKeyUrl(),
	}).Debug("Certificate info")

	return response.GetCertUrl(), response.GetKeyUrl(), nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (client *Client) createConnection(
	cryptocontext *cryptutils.CryptoContext, serverURL string, insecureCon bool,
) (connection *grpc.ClientConn, pbPublic pb.IAMPublicServiceClient, err error) {
	log.Debug("Connecting to IAM...")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	var secureOpt grpc.DialOption

	if insecureCon {
		secureOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		if cryptocontext == nil {
			return nil, nil, aoserrors.New("cryptocontext must not be nil")
		}

		tlsConfig, err := cryptocontext.GetClientTLSConfig()
		if err != nil {
			return nil, nil, aoserrors.Wrap(err)
		}

		secureOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	if connection, err = grpc.DialContext(ctx, serverURL,
		secureOpt, grpc.WithBlock()); err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	pbPublic = pb.NewIAMPublicServiceClient(connection)

	log.Debug("Connected to IAM")

	return connection, pbPublic, nil
}
