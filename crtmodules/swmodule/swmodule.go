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

package swmodule

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"time"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/crthandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// SWModule SW certificate module
type SWModule struct {
	crtType string
	config  moduleConfig
	storage crthandler.CrtStorage

	currentKey *rsa.PrivateKey
}

type moduleConfig struct {
	StoragePath string `json:"storagePath"`
	MaxItems    int    `json:"maxItems"`
}

/*******************************************************************************
 * Types
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates ssh module instance
func New(crtType string, configJSON json.RawMessage, storage crthandler.CrtStorage) (module crthandler.CrtModule, err error) {
	log.WithField("crtType", crtType).Info("Create SW module")

	swModule := &SWModule{crtType: crtType, storage: storage}

	if configJSON != nil {
		if err = json.Unmarshal(configJSON, &swModule.config); err != nil {
			return nil, err
		}
	}

	if err = os.MkdirAll(swModule.config.StoragePath, 0755); err != nil {
		return nil, err
	}

	return swModule, nil
}

// Close closes SW module
func (module *SWModule) Close() (err error) {
	log.WithField("crtType", module.crtType).Info("Close SW module")

	return err
}

// SyncStorage syncs cert storage
func (module *SWModule) SyncStorage() (err error) {
	log.WithFields(log.Fields{"crtType": module.crtType}).Debug("Sync storage")

	files, err := getFilesByExt(module.config.StoragePath, ".crt")
	if err != nil {
		return err
	}

	infos, err := module.storage.GetCertificates(module.crtType)
	if err != nil {
		return err
	}

	// Certs that no need to update

	var validURLs []string

	for _, file := range files {
		for _, info := range infos {
			if fileToURL(file) == info.CrtURL {
				validURLs = append(validURLs, info.CrtURL)
			}
		}
	}

	// FS certs that need to be updated

	var updateFiles []string

	for _, file := range files {
		found := false

		for _, validURL := range validURLs {
			if fileToURL(file) == validURL {
				found = true
			}
		}

		if !found {
			updateFiles = append(updateFiles, file)
		}
	}

	if err = module.updateCrts(updateFiles); err != nil {
		return err
	}

	// DB entries that should be removed

	var removeURLs []string

	for _, info := range infos {
		found := false

		for _, validURL := range validURLs {
			if info.CrtURL == validURL {
				found = true
			}
		}

		if !found {
			removeURLs = append(removeURLs, info.CrtURL)
		}
	}

	// Remove invalid DB entries

	for _, removeURL := range removeURLs {
		log.WithFields(log.Fields{"crtType": module.crtType, "crtURL": removeURL}).Warn("Remove invalid storage entry")

		if err = module.storage.RemoveCertificate(module.crtType, removeURL); err != nil {
			return err
		}
	}

	return nil
}

// CreateKeys creates key pair
func (module *SWModule) CreateKeys(systemID, password string) (csr string, err error) {
	log.WithFields(log.Fields{"crtType": module.crtType, "systemID": systemID}).Debug("Create keys")

	if module.currentKey != nil {
		log.Warning("Current key exists. Flushing...")
	}

	if module.currentKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		return "", err
	}

	csrDER, err := x509.CreateCertificateRequest(nil, &x509.CertificateRequest{Subject: pkix.Name{CommonName: systemID}}, module.currentKey)
	if err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})), nil
}

// ApplyCertificate applies certificate
func (module *SWModule) ApplyCertificate(crt string) (crtURL, keyURL string, err error) {
	if module.currentKey == nil {
		return "", "", errors.New("no key created")
	}
	defer func() { module.currentKey = nil }()

	block, _ := pem.Decode([]byte(crt))

	if block == nil {
		return "", "", errors.New("invalid PEM Block")
	}

	if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return "", "", errors.New("invalid PEM Block")
	}

	x509Crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", "", err
	}

	if err = checkCrt(x509Crt, module.currentKey.Public()); err != nil {
		return "", "", err
	}

	crtFileName, err := saveCrt(module.config.StoragePath, x509Crt)
	if err != nil {
		return "", "", err
	}

	keyFileName, err := saveKey(module.config.StoragePath, module.currentKey)
	if err != nil {
		return "", "", err
	}

	crtURL = fileToURL(crtFileName)
	keyURL = fileToURL(keyFileName)

	if err = module.storeCrt(x509Crt, crtURL, keyURL); err != nil {
		return "", "", err
	}

	crts, err := module.storage.GetCertificates(module.crtType)
	if err != nil {
		return "", "", err
	}

	for len(crts) > module.config.MaxItems && module.config.MaxItems != 0 {
		log.Warnf("Current cert count exceeds max count: %d > %d. Remove old certs", len(crts), module.config.MaxItems)

		if crts, err = module.removeOldestCertificate(crts); err != nil {
			return "", "", err
		}
	}

	return crtURL, keyURL, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func pemToX509Crt(crtPem string) (crt *x509.Certificate, err error) {
	block, _ := pem.Decode([]byte(crtPem))

	if block == nil {
		return nil, errors.New("invalid PEM Block")
	}

	if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return nil, errors.New("invalid PEM Block")
	}

	return x509.ParseCertificate(block.Bytes)
}

func checkCrt(crt *x509.Certificate, publicKey crypto.PublicKey) (err error) {
	pub, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}

	if !bytes.Equal(pub, crt.RawSubjectPublicKeyInfo) {
		return errors.New("certificate verification error")
	}

	return nil
}

func saveCrt(storageDir string, crt *x509.Certificate) (fileName string, err error) {
	file, err := ioutil.TempFile(storageDir, "*.crt")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err = pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: crt.Raw}); err != nil {
		return "", err
	}

	return file.Name(), nil
}

func saveKey(storageDir string, key *rsa.PrivateKey) (fileName string, err error) {
	file, err := ioutil.TempFile(storageDir, "*.key")
	if err != nil {
		return "", err
	}
	defer file.Close()

	err = pem.Encode(file, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err != nil {
		return "", err
	}

	return file.Name(), nil
}

func (module *SWModule) removeOldestCertificate(crts []crthandler.CrtInfo) (result []crthandler.CrtInfo, err error) {
	var minTime time.Time
	var minIndex int

	if len(crts) == 0 {
		return crts, nil
	}

	for i, crt := range crts {
		url, err := url.Parse(crt.CrtURL)
		if err != nil {
			return crts, err
		}

		crtPem, err := ioutil.ReadFile(url.Path)
		if err != nil {
			return crts, err
		}

		x509Crt, err := pemToX509Crt(string(crtPem))
		if err != nil {
			return crts, err
		}

		if minTime.IsZero() || x509Crt.NotAfter.Before(minTime) {
			minTime = x509Crt.NotAfter
			minIndex = i
		}
	}

	crtInfo := crts[minIndex]

	log.WithFields(log.Fields{
		"crtType":  module.crtType,
		"crtURL":   crtInfo.CrtURL,
		"keyURL":   crtInfo.KeyURL,
		"notAfter": minTime}).Debug("Remove certificate")

	if err = module.removeCrt(crtInfo); err != nil {
		return crts, err
	}

	if err = module.storage.RemoveCertificate(module.crtType, crtInfo.CrtURL); err != nil {
		return crts, err
	}

	return append(crts[:minIndex], crts[minIndex+1:]...), nil
}

func (module *SWModule) removeCrt(crt crthandler.CrtInfo) (err error) {
	keyURL, err := url.Parse(crt.KeyURL)
	if err != nil {
		return err
	}

	if err = os.Remove(keyURL.Path); err != nil {
		return err
	}

	crtURL, err := url.Parse(crt.CrtURL)
	if err != nil {
		return err
	}

	if err = os.Remove(crtURL.Path); err != nil {
		return err
	}

	return nil
}

func getFilesByExt(storagePath, ext string) (files []string, err error) {
	content, err := ioutil.ReadDir(storagePath)
	if err != nil {
		return nil, err
	}

	for _, item := range content {
		if item.IsDir() {
			continue
		}

		if path.Ext(item.Name()) != ext {
			continue
		}

		files = append(files, path.Join(storagePath, item.Name()))
	}

	return files, nil
}

func fileToURL(file string) (urlStr string) {
	urlVal := url.URL{Scheme: "file", Path: file}

	return urlVal.String()
}

func (module *SWModule) storeCrt(x509Crt *x509.Certificate, crtURL, keyURL string) (err error) {
	crtInfo := crthandler.CrtInfo{
		Issuer:   base64.StdEncoding.EncodeToString(x509Crt.RawIssuer),
		Serial:   fmt.Sprintf("%X", x509Crt.SerialNumber),
		CrtURL:   crtURL,
		KeyURL:   keyURL,
		NotAfter: x509Crt.NotAfter,
	}

	if err = module.storage.AddCertificate(module.crtType, crtInfo); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"crtType":  module.crtType,
		"issuer":   crtInfo.Issuer,
		"serial":   crtInfo.Serial,
		"crtURL":   crtInfo.CrtURL,
		"keyURL":   crtInfo.KeyURL,
		"notAfter": x509Crt.NotAfter}).Debug("Add certificate")

	return nil
}

func getCrtByURL(urlStr string) (x509Crt *x509.Certificate, err error) {
	urlVal, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	crtPem, err := ioutil.ReadFile(urlVal.Path)
	if err != nil {
		return nil, err
	}

	return pemToX509Crt(string(crtPem))
}

func getKeyByURL(urlStr string) (key *rsa.PrivateKey, err error) {
	urlVal, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	pemKey, err := ioutil.ReadFile(urlVal.Path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemKey)
	if err != nil {
		return nil, err
	}

	if block.Type != "RSA PRIVATE KEY" || len(block.Headers) != 0 {
		return nil, errors.New("invalid PEM Block")
	}

	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func (module *SWModule) updateCrts(files []string) (err error) {
	keyFiles, err := getFilesByExt(module.config.StoragePath, ".key")
	if err != nil {
		return err
	}

	for _, crtFile := range files {
		x509Crt, err := getCrtByURL(fileToURL(crtFile))
		if err != nil {
			return err
		}

		foundKeyFile := ""

		for _, keyFile := range keyFiles {
			key, err := getKeyByURL(fileToURL(keyFile))
			if err != nil {
				return err
			}

			if err = checkCrt(x509Crt, key.Public()); err == nil {
				foundKeyFile = keyFile

				break
			}
		}

		if foundKeyFile != "" {
			log.WithFields(log.Fields{"crtType": module.crtType, "file": crtFile}).Warn("Store valid certificate")

			if err = module.storeCrt(x509Crt, fileToURL(crtFile), fileToURL(foundKeyFile)); err != nil {
				return err
			}
		} else {
			log.WithFields(log.Fields{"crtType": module.crtType, "file": crtFile}).Warn("Remove invalid certificate")

			if err = os.Remove(crtFile); err != nil {
				return err
			}
		}
	}

	return nil
}
