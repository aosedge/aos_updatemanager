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

	"github.com/aoscloud/aos_common/aoserrors"
	pb "github.com/aoscloud/aos_common/api/iamanager/v4"
	"github.com/aoscloud/aos_common/utils/cryptutils"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aoscloud/aos_updatemanager/config"
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
	insecure     bool
	iamServerURL string
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new IAM client.
func New(config *config.Config, insecure bool) (client *Client, err error) {
	client = &Client{insecure: insecure, iamServerURL: config.IAMServerURL}

	return client, nil
}

// GetCertificate gets certificate by issuer.
func (client *Client) GetCertificate(
	cerType string, cryptocontext *cryptutils.CryptoContext,
) (certURL, keyURL string, err error) {
	log.WithFields(log.Fields{
		"type": cerType,
	}).Debug("Get certificate")

	connection, pbPublic, err := client.createConnection(cryptocontext)
	if err != nil {
		return "", "", err
	}

	defer func() {
		if connection != nil {
			connection.Close()
		}

		log.Debug("Disconnected from IAM")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	response, err := pbPublic.GetCert(ctx, &pb.GetCertRequest{Type: cerType})
	if err != nil {
		return "", "", aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"certURL": response.CertUrl, "keyURL": response.KeyUrl}).Debug("Certificate info")

	return response.CertUrl, response.KeyUrl, nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (client *Client) createConnection(
	cryptocontext *cryptutils.CryptoContext,
) (connection *grpc.ClientConn, pbPublic pb.IAMPublicServiceClient, err error) {
	log.Debug("Connecting to IAM...")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	var secureOpt grpc.DialOption

	if client.insecure {
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

	if connection, err = grpc.DialContext(ctx, client.iamServerURL, secureOpt, grpc.WithBlock()); err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	pbPublic = pb.NewIAMPublicServiceClient(connection)

	log.Debug("Connected to IAM")

	return connection, pbPublic, nil
}
