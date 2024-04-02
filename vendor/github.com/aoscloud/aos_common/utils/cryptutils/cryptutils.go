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

package cryptutils

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"sync"

	"github.com/ThalesIgnite/crypto11"
	"github.com/google/go-tpm/tpmutil"
	pkcs11uri "github.com/stefanberger/go-pkcs11uri"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/tpmkey"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// PEMExt PEM format extension.
const PEMExt = "pem"

// PEM block types.
const (
	PEMBlockRSAPrivateKey      = "RSA PRIVATE KEY"
	PEMBlockECPrivateKey       = "EC PRIVATE KEY"
	PEMBlockCertificate        = "CERTIFICATE"
	PEMBlockCertificateRequest = "CERTIFICATE REQUEST"
)

// Crypto algorithm.
const (
	AlgRSA = "rsa"
	AlgECC = "ecc"
)

// URL schemes.
const (
	SchemeFile   = "file"
	SchemeTPM    = "tpm"
	SchemePKCS11 = "pkcs11"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// CryptoContext crypt context.
type CryptoContext struct {
	sync.Mutex

	rootCertPool  *x509.CertPool
	tpmDevices    map[string]io.ReadWriteCloser
	pkcs11Ctx     map[pkcs11Descriptor]*crypto11.Context
	pkcs11Library string
}

type pkcs11Descriptor struct {
	library string
	token   string
}

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

//nolint:gochecknoglobals
var (
	// DefaultTPMDevice used if not specified in the URL.
	DefaultTPMDevice io.ReadWriteCloser
	// DefaultPKCS11Library used if not specified in the URL.
	DefaultPKCS11Library string
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// NewCryptoContext creates new crypto context.
func NewCryptoContext(rootCA string) (cryptoContext *CryptoContext, err error) {
	cryptoContext = &CryptoContext{
		pkcs11Ctx:  make(map[pkcs11Descriptor]*crypto11.Context),
		tpmDevices: make(map[string]io.ReadWriteCloser),
	}

	if rootCA != "" {
		if cryptoContext.rootCertPool, err = getCaCertPool(rootCA); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	return cryptoContext, nil
}

// Close closes crypto context.
func (cryptoContext *CryptoContext) Close() (err error) {
	cryptoContext.Lock()
	defer cryptoContext.Unlock()

	for _, tpmDevice := range cryptoContext.tpmDevices {
		if tpmErr := tpmDevice.Close(); tpmErr != nil {
			if err == nil {
				err = aoserrors.Wrap(tpmErr)
			}
		}
	}

	for _, pkcs11Ctx := range cryptoContext.pkcs11Ctx {
		if ctxErr := pkcs11Ctx.Close(); ctxErr != nil {
			if err == nil {
				err = aoserrors.Wrap(ctxErr)
			}
		}
	}

	return err
}

// GetCACertPool returns crypt context CA cert pool.
func (cryptoContext *CryptoContext) GetCACertPool() *x509.CertPool {
	return cryptoContext.rootCertPool
}

// LoadCertificateByURL loads certificate by URL.
func (cryptoContext *CryptoContext) LoadCertificateByURL(certURLStr string) ([]*x509.Certificate, error) {
	certURL, err := url.Parse(certURLStr)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	switch certURL.Scheme {
	case SchemeFile:
		certs, err := LoadCertificateFromFile(certURL.Path)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		return certs, nil

	case SchemePKCS11:
		certs, err := cryptoContext.loadCertificateFromPKCS11(certURL.String())
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		return certs, aoserrors.Wrap(err)

	default:
		return nil, aoserrors.Errorf("unsupported schema %s for certificate", certURL.Scheme)
	}
}

// LoadPrivateKeyByURL loads private key by URL.
func (cryptoContext *CryptoContext) LoadPrivateKeyByURL(keyURLStr string) (privKey crypto.PrivateKey,
	supportPKCS1v15SessionKey bool, err error,
) {
	keyURL, err := url.Parse(keyURLStr)
	if err != nil {
		return nil, false, aoserrors.Wrap(err)
	}

	switch keyURL.Scheme {
	case SchemeFile:
		if privKey, err = LoadPrivateKeyFromFile(keyURL.Path); err != nil {
			return nil, false, aoserrors.Wrap(err)
		}

		supportPKCS1v15SessionKey = true

	case SchemeTPM:
		if privKey, err = cryptoContext.loadPrivateKeyFromTPM(keyURL); err != nil {
			return nil, false, aoserrors.Wrap(err)
		}

	case SchemePKCS11:
		if privKey, err = cryptoContext.loadPrivateKeyFromPKCS11(keyURL.String()); err != nil {
			return nil, false, aoserrors.Wrap(err)
		}

	default:
		return nil, false, aoserrors.Errorf("unsupported schema %s for private key", keyURL.Scheme)
	}

	return privKey, supportPKCS1v15SessionKey, nil
}

// GetServerMutualTLSConfig returns server mutual TLS configuration.
func (cryptoContext *CryptoContext) GetServerMutualTLSConfig(certURLStr, keyURLStr string) (*tls.Config, error) {
	tlsCertificate, err := cryptoContext.getTLSCertificate(certURLStr, keyURLStr)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    cryptoContext.rootCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// GetServerTLSConfig returns server TLS configuration.
func (cryptoContext *CryptoContext) GetServerTLSConfig(certURLStr, keyURLStr string) (*tls.Config, error) {
	tlsCertificate, err := cryptoContext.getTLSCertificate(certURLStr, keyURLStr)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCertificate},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// GetClientMutualTLSConfig returns client mTLS config.
func (cryptoContext *CryptoContext) GetClientMutualTLSConfig(certURLStr, keyURLStr string) (*tls.Config, error) {
	tlsCertificate, err := cryptoContext.getTLSCertificate(certURLStr, keyURLStr)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return &tls.Config{
		RootCAs:      cryptoContext.rootCertPool,
		Certificates: []tls.Certificate{tlsCertificate},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// GetClientTLSConfig returns client TLS config.
func (cryptoContext *CryptoContext) GetClientTLSConfig() (*tls.Config, error) {
	return &tls.Config{RootCAs: cryptoContext.rootCertPool, MinVersion: tls.VersionTLS12}, nil
}

// CertToPEM is a utility function returns a PEM encoded x509 Certificate.
func CertToPEM(cert *x509.Certificate) []byte {
	pemCert := pem.EncodeToMemory(&pem.Block{Type: PEMBlockCertificate, Bytes: cert.Raw})

	return pemCert
}

// PEMToX509Cert parses PEM data to x509 certificate structures.
func PEMToX509Cert(data []byte) (certs []*x509.Certificate, err error) {
	var block *pem.Block

	for {
		block, data = pem.Decode(data)
		if block == nil {
			break
		}

		if block.Type == PEMBlockCertificate {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, aoserrors.Wrap(err)
			}

			certs = append(certs, cert)
		}
	}

	if len(certs) == 0 {
		return nil, aoserrors.Errorf("wrong certificate PEM format")
	}

	return certs, nil
}

// PEMToX509Key parses PEM data to x509 key structures.
func PEMToX509Key(data []byte) (key crypto.PrivateKey, err error) {
	block, _ := pem.Decode(data)
	if block != nil {
		data = block.Bytes
	}

	switch {
	case block == nil:
		if key, err = parseKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	case block.Type == PEMBlockRSAPrivateKey:
		if key, err = x509.ParsePKCS1PrivateKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	case block.Type == PEMBlockECPrivateKey:
		if key, err = x509.ParseECPrivateKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	default:
		if key, err = parseKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	return key, nil
}

// PEMToX509PrivateKey parses PEM data to x509 private key structures.
func PEMToX509PrivateKey(data []byte) (key crypto.PrivateKey, err error) {
	block, _ := pem.Decode(data)
	if block != nil {
		data = block.Bytes
	}

	switch {
	case block == nil:
		if key, err = parseKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	case block.Type == PEMBlockRSAPrivateKey:
		if key, err = x509.ParsePKCS1PrivateKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	case block.Type == PEMBlockECPrivateKey:
		if key, err = x509.ParseECPrivateKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}

	default:
		if key, err = parseKey(data); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	return key, nil
}

// LoadCertificateFromFile loads certificate from file.
func LoadCertificateFromFile(fileName string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	certs, err := PEMToX509Cert(data)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return certs, nil
}

// SaveCertificateToFile saves certificate to file.
func SaveCertificateToFile(fileName string, certs []*x509.Certificate) error {
	file, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer file.Close()

	for _, cert := range certs {
		if err = pem.Encode(file, &pem.Block{Type: PEMBlockCertificate, Bytes: cert.Raw}); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

// LoadPrivateKeyFromFile loads private key from file.
func LoadPrivateKeyFromFile(fileName string) (crypto.PrivateKey, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	key, err := PEMToX509PrivateKey(data)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return key, nil
}

// SavePrivateKeyToFile saves private key to file.
func SavePrivateKeyToFile(fileName string, key crypto.PrivateKey) error {
	keyPem, err := PrivateKeyToPEM(key)
	if err != nil {
		return err
	}

	if err := os.WriteFile(fileName, keyPem, 0o600); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// PrivateKeyToPEM converts private key to PEM format.
func PrivateKeyToPEM(key crypto.PrivateKey) ([]byte, error) {
	switch privateKey := key.(type) {
	case *rsa.PrivateKey:
		return pem.EncodeToMemory(&pem.Block{
			Type:  PEMBlockRSAPrivateKey,
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		}), nil

	case *ecdsa.PrivateKey:
		data, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		return pem.EncodeToMemory(&pem.Block{Type: PEMBlockECPrivateKey, Bytes: data}), nil

	default:
		return nil, aoserrors.Errorf("unsupported key type: %v", reflect.TypeOf(privateKey))
	}
}

// CheckCertificate checks if certificate matches key.
func CheckCertificate(cert *x509.Certificate, key crypto.PrivateKey) error {
	signer, ok := key.(crypto.Signer)
	if !ok {
		return aoserrors.New("private key does not implement public key interface")
	}

	pub, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if !bytes.Equal(pub, cert.RawSubjectPublicKeyInfo) {
		return aoserrors.New("certificate verification error")
	}

	return nil
}

// ParsePKCS11URL extracts library, token, label, id, user pin from pkcs URL.
func ParsePKCS11URL(pkcs11URL string) (library, token, userPIN string, label, id []byte, err error) {
	uri := pkcs11uri.New()

	uri.SetAllowAnyModule(true)

	if err := uri.Parse(pkcs11URL); err != nil {
		return "", "", "", nil, nil, aoserrors.Wrap(err)
	}

	if library, err = uri.GetModule(); err != nil {
		if err.Error() != "module-name attribute is not set" {
			return "", "", "", nil, nil, aoserrors.Wrap(err)
		}

		library = DefaultPKCS11Library
	}

	if uri.HasPIN() {
		if userPIN, err = uri.GetPIN(); err != nil {
			return "", "", "", nil, nil, aoserrors.Wrap(err)
		}
	}

	token, _ = uri.GetPathAttribute("token", false)
	labelStr, _ := uri.GetPathAttribute("object", false)
	label = []byte(labelStr)
	idStr, _ := uri.GetPathAttribute("id", false)
	id = []byte(idStr)

	return library, token, userPIN, label, id, nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func getCaCertPool(rootCaFilePath string) (*x509.CertPool, error) {
	pemCA, err := os.ReadFile(rootCaFilePath)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	caCertPool := x509.NewCertPool()

	if !caCertPool.AppendCertsFromPEM(pemCA) {
		return nil, aoserrors.New("failed to add CA's certificate")
	}

	return caCertPool, nil
}

func getRawCertificate(certs []*x509.Certificate) [][]byte {
	rawCerts := make([][]byte, 0, len(certs))

	for _, cert := range certs {
		rawCerts = append(rawCerts, cert.Raw)
	}

	return rawCerts
}

func (cryptoContext *CryptoContext) getTLSCertificate(certURLStr, keyURLStr string) (tls.Certificate, error) {
	certs, err := cryptoContext.LoadCertificateByURL(certURLStr)
	if err != nil {
		return tls.Certificate{}, aoserrors.Wrap(err)
	}

	key, _, err := cryptoContext.LoadPrivateKeyByURL(keyURLStr)
	if err != nil {
		return tls.Certificate{}, aoserrors.Wrap(err)
	}

	return tls.Certificate{Certificate: getRawCertificate(certs), PrivateKey: key}, nil
}

func (cryptoContext *CryptoContext) getPKCS11Context(library, token, userPin string) (*crypto11.Context, error) {
	cryptoContext.Lock()
	defer cryptoContext.Unlock()

	if library == "" && cryptoContext.pkcs11Library == "" {
		return nil, aoserrors.New("PKCS11 library is not defined")
	}

	if library == "" {
		library = cryptoContext.pkcs11Library
	}

	pkcs11Desc := pkcs11Descriptor{library: library, token: token}

	pkcs11Ctx, ok := cryptoContext.pkcs11Ctx[pkcs11Desc]
	if !ok {
		var err error

		if pkcs11Ctx, err = crypto11.Configure(&crypto11.Config{
			Path: library, TokenLabel: token, Pin: userPin,
		}); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		cryptoContext.pkcs11Ctx[pkcs11Desc] = pkcs11Ctx
	}

	return pkcs11Ctx, nil
}

func (cryptoContext *CryptoContext) loadCertificateFromPKCS11(certURL string) ([]*x509.Certificate, error) {
	library, token, userPIN, label, id, err := ParsePKCS11URL(certURL)
	if err != nil {
		return nil, err
	}

	pkcs11Ctx, err := cryptoContext.getPKCS11Context(library, token, userPIN)
	if err != nil {
		return nil, err
	}

	certs, err := pkcs11Ctx.FindCertificateChain(id, label, nil)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if len(certs) == 0 {
		return nil, aoserrors.Errorf("certificate chain label: %s, id: %s not found", label, id)
	}

	return certs, nil
}

func (cryptoContext *CryptoContext) getTPMDevice(device string) (io.ReadWriteCloser, error) {
	cryptoContext.Lock()
	defer cryptoContext.Unlock()

	if device == "" {
		if DefaultTPMDevice == nil {
			return nil, aoserrors.New("default TPM device is not defined")
		}

		return DefaultTPMDevice, nil
	}

	tpmDevice, ok := cryptoContext.tpmDevices[device]
	if ok {
		return tpmDevice, nil
	}

	tpmDevice, err := tpmutil.OpenTPM(device)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return tpmDevice, nil
}

func (cryptoContext *CryptoContext) loadPrivateKeyFromTPM(keyURL *url.URL) (crypto.PrivateKey, error) {
	tpmDevice, err := cryptoContext.getTPMDevice(keyURL.Hostname())
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	handle, err := strconv.ParseUint(keyURL.Opaque, 0, 32)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	privKey, err := tpmkey.CreateFromPersistent(tpmDevice, tpmutil.Handle(handle))
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return privKey, nil
}

func (cryptoContext *CryptoContext) loadPrivateKeyFromPKCS11(keyURL string) (crypto.PrivateKey, error) {
	library, token, userPIN, label, id, err := ParsePKCS11URL(keyURL)
	if err != nil {
		return nil, err
	}

	pkcs11Ctx, err := cryptoContext.getPKCS11Context(library, token, userPIN)
	if err != nil {
		return nil, err
	}

	key, err := pkcs11Ctx.FindKeyPair(id, label)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if key == nil {
		return nil, aoserrors.Errorf("private key label: %s, id: %s not found", label, id)
	}

	return key, nil
}

func parseKey(data []byte) (crypto.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(data); err == nil {
		return key, nil
	}

	if key, err := x509.ParseECPrivateKey(data); err == nil {
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(data); err == nil {
		return key, nil
	}

	return nil, aoserrors.New("invalid key format")
}
