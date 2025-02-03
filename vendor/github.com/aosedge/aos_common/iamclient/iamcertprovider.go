// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2025 EPAM Systems, Inc.
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

	"github.com/aosedge/aos_common/aoserrors"
	pb "github.com/aosedge/aos_common/api/iamanager"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// IAM certificate provider.
type IAMCertProvider struct {
	connection *grpc.ClientConn
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// NewCertProvider creates new certificate provider instance.
func NewCertProvider(connection *grpc.ClientConn) *IAMCertProvider {
	return &IAMCertProvider{connection: connection}
}

// GetCertificate gets certificate by issuer.
func (provider *IAMCertProvider) GetCertificate(
	certType string, issuer []byte, serial string,
) (certURL, keyURL string, err error) {
	log.Debug("Get IAM certificate")

	ctx, cancel := context.WithTimeout(context.Background(), iamRequestTimeout)
	defer cancel()

	publicService := pb.NewIAMPublicServiceClient(provider.connection)

	response, err := publicService.GetCert(
		ctx, &pb.GetCertRequest{Type: certType, Issuer: issuer, Serial: serial})
	if err != nil {
		return "", "", aoserrors.Wrap(err)
	}

	return response.GetCertUrl(), response.GetKeyUrl(), nil
}
