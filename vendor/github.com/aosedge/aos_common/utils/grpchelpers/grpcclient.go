// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
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

package grpchelpers

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/utils/cryptutils"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// CertProvider certificate provider interface.
type CertProvider interface {
	GetCertificate(certType string, issuer []byte, serial string) (certURL, keyURL string, err error)
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// CreatePublicConnection creates public GRPC connection.
func CreatePublicConnection(serverURL string, cryptocontext *cryptutils.CryptoContext, insecureConn bool) (
	connection *grpc.ClientConn, err error,
) {
	var secureOpt grpc.DialOption

	if insecureConn {
		secureOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		tlsConfig, err := cryptocontext.GetClientTLSConfig()
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		secureOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	if connection, err = grpc.NewClient(serverURL, secureOpt); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return connection, nil
}

// CreateProtectedConnection creates protected GRPC connection.
func CreateProtectedConnection(
	certType string, protectedURL string, cryptocontext *cryptutils.CryptoContext,
	certProvider CertProvider, insecureConn bool,
) (connection *grpc.ClientConn, err error) {
	var secureOpt grpc.DialOption

	if insecureConn {
		secureOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		certURL, keyURL, err := certProvider.GetCertificate(certType, nil, "")
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		tlsConfig, err := cryptocontext.GetClientMutualTLSConfig(certURL, keyURL)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		secureOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	}

	if connection, err = grpc.NewClient(protectedURL, secureOpt); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return connection, nil
}
