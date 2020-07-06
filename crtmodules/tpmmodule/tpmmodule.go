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

package tpmmodule

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpmutil"
	log "github.com/sirupsen/logrus"

	"aos_updatemanager/crthandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// TPMModule TPM certificate module
type TPMModule struct {
	crtType string
	config  moduleConfig
	device  io.ReadWriteCloser
	storage crthandler.CrtStorage

	currentKey *key
}

type moduleConfig struct {
	Device      string `json:"device"`
	StoragePath string `json:"storagePath"`
	MaxItems    int    `json:"maxItems"`
}

type key struct {
	device    io.ReadWriteCloser
	handle    tpmutil.Handle
	pub       tpm2.Public
	publicKey crypto.PublicKey
	password  string
}

/*******************************************************************************
 * Types
 ******************************************************************************/

var supportedHash = map[crypto.Hash]tpm2.Algorithm{
	crypto.SHA1:   tpm2.AlgSHA1,
	crypto.SHA256: tpm2.AlgSHA256,
	crypto.SHA384: tpm2.AlgSHA384,
	crypto.SHA512: tpm2.AlgSHA512,
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates ssh module instance
func New(crtType string, configJSON json.RawMessage, storage crthandler.CrtStorage,
	device io.ReadWriteCloser) (module crthandler.CrtModule, err error) {
	log.WithField("crtType", crtType).Info("Create TPM module")

	tpmModule := &TPMModule{crtType: crtType, storage: storage}

	if configJSON != nil {
		if err = json.Unmarshal(configJSON, &tpmModule.config); err != nil {
			return nil, err
		}
	}

	if device == nil {
		if tpmModule.config.Device == "" {
			return nil, errors.New("TPM device should be set")
		}

		if tpmModule.device, err = tpm2.OpenTPM(tpmModule.config.Device); err != nil {
			return nil, err
		}
	} else {
		tpmModule.device = device
	}

	if err = os.MkdirAll(tpmModule.config.StoragePath, 0755); err != nil {
		return nil, err
	}

	return tpmModule, nil
}

// Close closes TPM module
func (module *TPMModule) Close() (err error) {
	log.WithField("crtType", module.crtType).Info("Close TPM module")

	if module.currentKey != nil {
		if flushErr := module.currentKey.flush(); flushErr != nil {
			if err == nil {
				err = flushErr
			}
		}
	}

	if module.device != nil {
		if closeErr := module.device.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}

	return err
}

// SyncStorage syncs cert storage
func (module *TPMModule) SyncStorage() (err error) {
	log.WithFields(log.Fields{"crtType": module.crtType}).Debug("Sync storage")

	files, err := getCrtFiles(module.config.StoragePath)
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
			if fileToCrtURL(file) == info.CrtURL {
				validURLs = append(validURLs, info.CrtURL)
			}
		}
	}

	// FS certs that need to be updated

	var updateFiles []string

	for _, file := range files {
		found := false

		for _, validURL := range validURLs {
			if fileToCrtURL(file) == validURL {
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
func (module *TPMModule) CreateKeys(systemID, password string) (csr string, err error) {
	log.WithFields(log.Fields{"crtType": module.crtType, "systemID": systemID}).Debug("Create keys")

	if module.currentKey != nil {
		log.Warning("Current key exists. Flushing...")

		if err = module.currentKey.flush(); err != nil {
			return "", err
		}
	}

	if module.currentKey, err = module.newKey(password); err != nil {
		return "", err
	}

	csrDER, err := x509.CreateCertificateRequest(nil, &x509.CertificateRequest{Subject: pkix.Name{CommonName: systemID}}, module.currentKey)
	if err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})), nil
}

// ApplyCertificate applies certificate
func (module *TPMModule) ApplyCertificate(crt string) (crtURL, keyURL string, err error) {
	if module.currentKey == nil {
		return "", "", errors.New("no key created")
	}
	defer func() { module.currentKey = nil }()

	x509Crt, err := pemToX509Crt(crt)
	if err != nil {
		return "", "", nil
	}

	if err = checkCrt(x509Crt, module.currentKey.publicKey); err != nil {
		return "", "", err
	}

	crtFileName, err := saveCrt(module.config.StoragePath, crt)
	if err != nil {
		return "", "", err
	}

	persistentHandle, err := module.findEmptyPersistentHandle()
	if err != nil {
		return "", "", err
	}

	if err = module.currentKey.makePersistent(persistentHandle); err != nil {
		return "", "", err
	}

	crtURL = fileToCrtURL(crtFileName)
	keyURL = handleToKeyURL(persistentHandle)

	if err = module.addCrtToDB(x509Crt, crtURL, keyURL); err != nil {
		return "", "", err
	}

	crts, err := module.storage.GetCertificates(module.crtType)
	if err != nil {
		return "", "", err
	}

	for len(crts) > module.config.MaxItems && module.config.MaxItems != 0 {
		log.Warnf("Current cert count exceeds max count: %d > %d. Remove old certs", len(crts), module.config.MaxItems)

		if crts, err = module.removeOldestCertificate(crts, module.currentKey.password); err != nil {
			log.Errorf("Can't delete old certificate: %s", err)
		}
	}

	return crtURL, keyURL, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (module *TPMModule) newKey(password string) (k *key, err error) {
	k = &key{device: module.device, password: password}

	primaryKeyTemplate := tpm2.Public{
		Type:    tpm2.AlgRSA,
		NameAlg: tpm2.AlgSHA256,
		Attributes: tpm2.FlagFixedTPM | tpm2.FlagFixedParent | tpm2.FlagSensitiveDataOrigin |
			tpm2.FlagUserWithAuth | tpm2.FlagRestricted | tpm2.FlagDecrypt,
		RSAParameters: &tpm2.RSAParams{
			Symmetric: &tpm2.SymScheme{
				Alg:     tpm2.AlgAES,
				KeyBits: 128,
				Mode:    tpm2.AlgCFB,
			},
			KeyBits: 2048,
		},
	}

	keyTemplate := tpm2.Public{
		Type:    tpm2.AlgRSA,
		NameAlg: tpm2.AlgSHA256,
		Attributes: tpm2.FlagFixedTPM | tpm2.FlagFixedParent | tpm2.FlagSensitiveDataOrigin |
			tpm2.FlagUserWithAuth | tpm2.FlagDecrypt | tpm2.FlagSign,
		RSAParameters: &tpm2.RSAParams{
			Sign: &tpm2.SigScheme{
				Alg:  tpm2.AlgNull,
				Hash: tpm2.AlgNull,
			},
			KeyBits: 2048,
		},
	}

	primaryHandle, _, err := tpm2.CreatePrimary(module.device, tpm2.HandleOwner, tpm2.PCRSelection{}, k.password, k.password, primaryKeyTemplate)
	if err != nil {
		return nil, err
	}
	defer tpm2.FlushContext(module.device, primaryHandle)

	privateBlob, publicBlob, _, _, _, err := tpm2.CreateKey(module.device, primaryHandle, tpm2.PCRSelection{}, k.password, "", keyTemplate)
	if err != nil {
		return nil, err
	}

	if k.pub, err = tpm2.DecodePublic(publicBlob); err != nil {
		return nil, err
	}

	if k.publicKey, err = k.pub.Key(); err != nil {
		return nil, err
	}

	if k.handle, _, err = tpm2.Load(module.device, primaryHandle, k.password, publicBlob, privateBlob); err != nil {
		return nil, err
	}

	return k, nil
}

func (k *key) flush() (err error) {
	return tpm2.FlushContext(k.device, k.handle)
}

func (k *key) makePersistent(handle tpmutil.Handle) (err error) {
	// Clear slot
	tpm2.EvictControl(k.device, k.password, tpm2.HandleOwner, handle, handle)

	if err = tpm2.EvictControl(k.device, k.password, tpm2.HandleOwner, k.handle, handle); err != nil {
		return err
	}

	k.flush()

	k.handle = handle

	return nil
}

func (k *key) Public() crypto.PublicKey {
	return k.publicKey
}

func (k *key) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	alg := tpm2.AlgRSASSA

	if pssOpts, ok := opts.(*rsa.PSSOptions); ok {
		if pssOpts.SaltLength != rsa.PSSSaltLengthAuto {
			return nil, fmt.Errorf("salt length must be rsa.PSSSaltLengthAuto")
		}

		alg = tpm2.AlgRSAPSS
	}

	tpmHash, ok := supportedHash[opts.HashFunc()]
	if !ok {
		return nil, fmt.Errorf("unsupported hash algorithm: %v", opts.HashFunc())
	}

	if len(digest) != opts.HashFunc().Size() {
		return nil, fmt.Errorf("wrong digest length: got %d, want %d", digest, opts.HashFunc().Size())
	}

	scheme := &tpm2.SigScheme{
		Alg:  alg,
		Hash: tpmHash,
	}

	sig, err := tpm2.Sign(k.device, k.handle, "", digest, nil, scheme)
	if err != nil {
		return nil, err
	}

	switch sig.Alg {
	case tpm2.AlgRSASSA:
		return sig.RSA.Signature, nil

	case tpm2.AlgRSAPSS:
		return sig.RSA.Signature, nil

	case tpm2.AlgECDSA:
		sigStruct := struct{ R, S *big.Int }{sig.ECC.R, sig.ECC.S}

		return asn1.Marshal(sigStruct)

	default:
		return nil, errors.New("unsupported signing algorithm")
	}
}

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

func saveCrt(storageDir string, crt string) (fileName string, err error) {
	file, err := ioutil.TempFile(storageDir, "*.crt")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err = file.WriteString(crt); err != nil {
		return "", err
	}

	return file.Name(), nil
}

func (module *TPMModule) findEmptyPersistentHandle() (handle tpmutil.Handle, err error) {
	values, _, err := tpm2.GetCapability(module.device, tpm2.CapabilityHandles,
		uint32(tpm2.PersistentLast)-uint32(tpm2.PersistentFirst), uint32(tpm2.PersistentFirst))
	if err != nil {
		return 0, err
	}

	if len(values) == 0 {
		return tpmutil.Handle(tpm2.PersistentFirst), nil
	}

	for i := tpmutil.Handle(tpm2.PersistentFirst); i < tpmutil.Handle(tpm2.PersistentLast); i++ {
		inUse := false

		for _, value := range values {
			handle, ok := value.(tpmutil.Handle)
			if !ok {
				return 0, errors.New("wrong data format")
			}

			if i == handle {
				inUse = true
			}
		}

		if !inUse {
			return i, nil
		}
	}

	return 0, errors.New("no empty persistent slot found")
}

func (module *TPMModule) removeOldestCertificate(crts []crthandler.CrtInfo, password string) (result []crthandler.CrtInfo, err error) {
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

		pemCrt, err := ioutil.ReadFile(url.Path)
		if err != nil {
			return crts, err
		}

		x509Crt, err := pemToX509Crt(string(pemCrt))
		if err != nil {
			return crts, nil
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

	if err = module.removeCrt(crtInfo, password); err != nil {
		return crts, err
	}

	if err = module.storage.RemoveCertificate(module.crtType, crtInfo.CrtURL); err != nil {
		return crts, err
	}

	return append(crts[:minIndex], crts[minIndex+1:]...), nil
}

func (module *TPMModule) removeCrt(crt crthandler.CrtInfo, password string) (err error) {
	keyURL, err := url.Parse(crt.KeyURL)
	if err != nil {
		return err
	}

	handle, err := strconv.ParseUint(keyURL.Hostname(), 0, 32)
	if err != nil {
		return err
	}

	if err = tpm2.EvictControl(module.device, password, tpm2.HandleOwner, tpmutil.Handle(handle), tpmutil.Handle(handle)); err != nil {
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

func getCrtFiles(storagePath string) (files []string, err error) {
	content, err := ioutil.ReadDir(storagePath)
	if err != nil {
		return nil, err
	}

	for _, item := range content {
		if item.IsDir() {
			continue
		}

		files = append(files, path.Join(storagePath, item.Name()))
	}

	return files, nil
}

func fileToCrtURL(file string) (crtURL string) {
	urlVal := url.URL{Scheme: "file", Path: file}

	return urlVal.String()
}

func handleToKeyURL(handle tpmutil.Handle) (keyURL string) {
	urlVal := url.URL{Scheme: "tpm", Host: fmt.Sprintf("0x%X", handle)}

	return urlVal.String()
}

func (module *TPMModule) addCrtToDB(x509Crt *x509.Certificate, crtURL, keyURL string) (err error) {
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

func (module *TPMModule) getPersistentHandles() (handles []tpmutil.Handle, err error) {
	values, _, err := tpm2.GetCapability(module.device, tpm2.CapabilityHandles,
		uint32(tpm2.PersistentLast)-uint32(tpm2.PersistentFirst), uint32(tpm2.PersistentFirst))
	if err != nil {
		return nil, err
	}

	for _, value := range values {
		handle, ok := value.(tpmutil.Handle)
		if !ok {
			return nil, errors.New("wrong TPM data format")
		}

		handles = append(handles, handle)
	}

	return handles, nil
}

func (module *TPMModule) updateCrts(files []string) (err error) {
	pubs := make(map[tpmutil.Handle]tpm2.Public)

	handles, err := module.getPersistentHandles()
	if err != nil {
		return err
	}

	for _, handle := range handles {
		pub, _, _, err := tpm2.ReadPublic(module.device, handle)
		if err != nil {
			return err
		}

		pubs[handle] = pub
	}

	for _, crtFile := range files {
		crtPem, err := ioutil.ReadFile(crtFile)
		if err != nil {
			return err
		}

		x509Crt, err := pemToX509Crt(string(crtPem))
		if err != nil {
			return err
		}

		var crtHandle tpmutil.Handle

		for handle, pub := range pubs {
			publicKey, err := pub.Key()
			if err != nil {
				return err
			}

			if err = checkCrt(x509Crt, publicKey); err == nil {
				crtHandle = handle
			}
		}

		if crtHandle != 0 {
			log.WithFields(log.Fields{"crtType": module.crtType, "file": crtFile}).Warn("Store valid certificate")

			if err = module.addCrtToDB(x509Crt, fileToCrtURL(crtFile), handleToKeyURL(crtHandle)); err != nil {
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
