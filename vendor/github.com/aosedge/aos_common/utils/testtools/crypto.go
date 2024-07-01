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

package testtools

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/utils/cryptutils"
)

/***********************************************************************************************************************
 * Var
 **********************************************************************************************************************/

// DefaultCertificateTemplate default certificate template.
var DefaultCertificateTemplate = x509.Certificate{ //nolint:gochecknoglobals
	SerialNumber: big.NewInt(1),
	Version:      3, //nolint:gomnd
	NotBefore:    time.Now().Add(-10 * time.Second),
	NotAfter:     time.Now().AddDate(10, 0, 0),
	KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature |
		x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	Subject: pkix.Name{
		Country:            []string{"UA"},
		Organization:       []string{"EPAM"},
		OrganizationalUnit: []string{"Aos"},
		Locality:           []string{"Kyiv"},
	},
	DNSNames: []string{
		"localhost",
		"wwwaosum",
		"*.kyiv.epam.com",
		"*.aos-dev.test",
	},
	BasicConstraintsValid: true,
	IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1).To4(), net.IPv6loopback}, //nolint:gomnd
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// CreateCSR creates CSR.
func CreateCSR(key crypto.PrivateKey) (csr []byte, err error) {
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	csr = pem.EncodeToMemory(&pem.Block{Type: cryptutils.PEMBlockCertificateRequest, Bytes: csrDER})

	return csr, nil
}

// GenerateCert generates certificate.
func GenerateCert(
	template, parent *x509.Certificate, privateKey crypto.PrivateKey, publicKey crypto.PublicKey,
) (*x509.Certificate, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	subjectKeyID := sha256.Sum256(publicKeyBytes)
	template.SubjectKeyId = subjectKeyID[:]

	caBytes, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, privateKey)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	cert, err := x509.ParseCertificate(caBytes)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return cert, nil
}

// GenerateCertAndKey generates key and certificate.
func GenerateCertAndKey(
	template, parent *x509.Certificate, parentPrivateKey crypto.PrivateKey,
) (cert *x509.Certificate, privateKey crypto.PrivateKey, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048) //nolint:gomnd
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	if parentPrivateKey == nil {
		parentPrivateKey = key
	}

	if cert, err = GenerateCert(template, parent, parentPrivateKey, key.Public()); err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

// VerifyCertChain verifies chain of certificates.
func VerifyCertChain(leafCert *x509.Certificate, roots, intermediates []*x509.Certificate) error {
	rootPool := x509.NewCertPool()
	intermediatePool := x509.NewCertPool()

	for _, c := range roots {
		rootPool.AddCert(c)
	}

	for _, c := range intermediates {
		intermediatePool.AddCert(c)
	}

	opts := x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
	}

	if _, err := leafCert.Verify(opts); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// GenerateDefaultCARootCertAndKey generates default CA root certificate and key.
func GenerateDefaultCARootCertAndKey() (cert *x509.Certificate, key crypto.PrivateKey, err error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	rootTemplate := DefaultCertificateTemplate
	rootTemplate.SerialNumber = serialNumber
	rootTemplate.IsCA = true
	rootTemplate.Subject.OrganizationalUnit = []string{"Novus Ordo Seclorum"}
	rootTemplate.Subject.CommonName = "Fusion Root CA"

	cert, key, err = GenerateCertAndKey(&rootTemplate, &rootTemplate, nil)
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	return cert, key, nil
}

// GenerateCACertAndKey generates CA certificate and key.
func GenerateCACertAndKey(
	parent *x509.Certificate, privateKey crypto.PrivateKey, subject pkix.Name,
) (*x509.Certificate, crypto.PrivateKey, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	template := DefaultCertificateTemplate
	template.SerialNumber = serialNumber
	template.Subject = subject
	template.IsCA = true

	cert, key, err := GenerateCertAndKey(&template, parent, privateKey)
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	return cert, key, nil
}

// GenerateCertAndKeyWithSubject generates certificate and key.
func GenerateCertAndKeyWithSubject(
	subject pkix.Name, parent *x509.Certificate, privateKey crypto.PrivateKey,
) (*x509.Certificate, crypto.PrivateKey, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	template := DefaultCertificateTemplate
	template.SerialNumber = serialNumber
	template.Subject = subject
	template.NotAfter = time.Now().AddDate(1, 0, 0)
	template.IsCA = false

	cert, key, err := GenerateCertAndKey(&template, parent, privateKey)
	if err != nil {
		return nil, nil, aoserrors.Wrap(err)
	}

	return cert, key, nil
}
