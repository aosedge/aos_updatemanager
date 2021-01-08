// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Renesas Inc.
// Copyright 2021 EPAM Systems Inc.
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

package cryptutils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
)

const (
	crtExt = ".crt"
	keyExt = ".key"
)

// GetCertFileFromDir returns first certificate file from directory
func GetCertFileFromDir(storageDir string) (crtFile string, err error) {
	return getFileByExtension(storageDir, crtExt)
}

// GetKeyFileFromDir returns first key file from directory
func GetKeyFileFromDir(storageDir string) (keyFile string, err error) {
	return getFileByExtension(storageDir, keyExt)
}

// GetClientTLSConfig returns client TLS configuration
func GetClientTLSConfig(CACert, certStorageDir string) (config *tls.Config, err error) {
	pemCA, err := ioutil.ReadFile(CACert)
	if err != nil {
		return config, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemCA) {
		return config, fmt.Errorf("failed to add CA's certificate")
	}

	clientCert, err := getKeyPairFromDir(certStorageDir)
	if err != nil {
		return config, err
	}

	config = &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return config, nil
}

// GetServerTLSConfig returns server TLS configuration
func GetServerTLSConfig(CACert, certStorageDir string) (config *tls.Config, err error) {
	pemCA, err := ioutil.ReadFile(CACert)
	if err != nil {
		return config, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemCA) {
		return config, fmt.Errorf("failed to add CA's certificate")
	}

	serverCert, err := getKeyPairFromDir(certStorageDir)
	if err != nil {
		return config, err
	}

	config = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	}

	return config, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func getFileByExtension(dir, ext string) (resultFile string, err error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(file.Name()) == ext {
			return path.Join(dir, file.Name()), nil
		}
	}

	return "", fmt.Errorf("no *%s files in %s", ext, dir)
}

func getKeyPairFromDir(dir string) (cert tls.Certificate, err error) {
	crtFile, err := getFileByExtension(dir, crtExt)
	if err != nil {
		return cert, err
	}

	keyFile, err := getFileByExtension(dir, keyExt)
	if err != nil {
		return cert, err
	}

	return tls.LoadX509KeyPair(crtFile, keyFile)
}
