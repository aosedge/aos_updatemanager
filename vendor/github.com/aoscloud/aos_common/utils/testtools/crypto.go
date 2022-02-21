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
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"os"
	"os/exec"
	"path"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/utils/cryptutils"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const filePerm = 0o600

const caCertificate = `-----BEGIN CERTIFICATE-----
MIIDYTCCAkmgAwIBAgIUefLO+XArcR2jeqrgGqQlTM20N/swDQYJKoZIhvcNAQEL
BQAwQDELMAkGA1UEBhMCVUExEzARBgNVBAgMClNvbWUtU3RhdGUxDTALBgNVBAcM
BEt5aXYxDTALBgNVBAoMBEVQQU0wHhcNMjAwNzAzMTU0NzEzWhcNMjAwODAyMTU0
NzEzWjBAMQswCQYDVQQGEwJVQTETMBEGA1UECAwKU29tZS1TdGF0ZTENMAsGA1UE
BwwES3lpdjENMAsGA1UECgwERVBBTTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBAMQAdRAyH+S6QSCYAq09Kn5uhNgBSEcOwUTdTH1W9BSgXaHAbHQmY0py
2EnXoQ4/B+xdFsFLRpW7dvDaXcgMjjX1B/Yn52lF2OLdTaRwcA5/5wU2hAKAs4lu
lLRS1Ez48cRutjyVwzB70EB78Og/79SbZnrE73RhE4gUGq1/7l95VrQeVyMxXPSz
T5DVQrwZ/TnNDHbB2WDP3oWi4EhHRSE3GxO9OvVIlWtps2/VLLGDjFKDDw57c+CJ
GtYDDSQGSAzYgKHLbC4YZdatLCzLOK+HYMBMQ+A+h1FFDOQiafjc2hhNAJJgK4YE
S2bTKPSDwUFvNXlojLUuRqmeJblTfU8CAwEAAaNTMFEwHQYDVR0OBBYEFGTTfDCg
4dwM/qAGCsMIt3akp3kaMB8GA1UdIwQYMBaAFGTTfDCg4dwM/qAGCsMIt3akp3ka
MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBACE7Dm84YTAhxnjf
BC5tekchab1LtG00kGD3olYIrk60ST9CTkeZKbIRUiJguu7YyS8/9acCI5PrL+Bj
JeThNo8BiHgEJ0MZkUI9JhIqWT1dHFZoiIWBJia6IyEEFrUEfKBpYW85Get25em3
xokm39qQ2HFKJXbzixE/4F792lUWU49g4tvClrkRrVISBxy1xPAQZ38dep9NMhHe
puBh64yKH073veYqAlkv4p+m0VDJsSRhrhHnC1n37P6UIy3FhyxfsnQ4JTbDsjyH
d43D/UeLrvqwwJvRWqwa1XCbkxyhBQ+/2Soq/ym+EFTgJJcT/UjXZMU6C3NF7oLa
2bbVjCU=
-----END CERTIFICATE-----`

const caKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAxAB1EDIf5LpBIJgCrT0qfm6E2AFIRw7BRN1MfVb0FKBdocBs
dCZjSnLYSdehDj8H7F0WwUtGlbt28NpdyAyONfUH9ifnaUXY4t1NpHBwDn/nBTaE
AoCziW6UtFLUTPjxxG62PJXDMHvQQHvw6D/v1JtmesTvdGETiBQarX/uX3lWtB5X
IzFc9LNPkNVCvBn9Oc0MdsHZYM/ehaLgSEdFITcbE7069UiVa2mzb9UssYOMUoMP
Dntz4Ika1gMNJAZIDNiAoctsLhhl1q0sLMs4r4dgwExD4D6HUUUM5CJp+NzaGE0A
kmArhgRLZtMo9IPBQW81eWiMtS5GqZ4luVN9TwIDAQABAoIBADsH0DnyfryKg/bn
EVdPpq6xZn0P1c7g2MB+zfyp5ZUYv1pp87//l8PiVtXWhYEe5qn/V00b+MQ705Sy
j7AiZ+pERAOU/RMtoCajdDDkVDtpthBR3OxMCsaHcW3lzF7qUxZQKb6RdFnz0EK7
kVDBgN/Ndc3f5iZs3k8LjwVWFFrYTzkM+5vD7W5u0ORwOPuvXvoR39fIbKAd2DcD
Q++lEt/+E1Uqggenpuyewgr/gg5OTIN9ky3bksjSnjqfb5ClmNypEp0oLV3aF/y+
B4GiZjckkWEFZX9gtqP+6TGb4IQVnSJz7k7n3vwOf5VUQjgZYx/363WdGalIUG79
NkGDwOECgYEA8hFX5Dy+Wiap43lc+P6MH1lxNKCwjMi5SvM/mYpi3vFIuHo8FxTW
HLKOqB82l/9N8WnC76JWq4raPyOKt+pyQKknSg3iArxks7fy4h/k+5F2c6/cFb6P
TaFDt7rG3B7x9gcJbNj2U0mMEn77vcYZ39DABv1yVQumOQA3wvcST9cCgYEAz0hf
Tbf/QILO4oQ/Olv9bN0oOVkwmm61RZyQHSykX3seTgNkkJ1EZRuqHPUIJGe0pc5/
jMQfK9WthYjx28YNnXlNCwWYf7M7n1rN2DsZciT0uQIio65GiDK4w3oRhTAWgX7L
QiH5eY6MxXMj68lHhTuFI/wSPeiksdFgtvnX70kCgYBUX30uHYoPrChNFFE2rKq0
hp1xxYykFZaYLD7/yn95y8oYGur09JtIt2gH65FA24kUW1PJ6OCivCwkE8RXJI2c
Qhlis4ISiA3lonkzHgDXOrV5z1M79QbH/Sy4To7fzJ1zrrI3UUxSbXE4RTCDzhfY
rk8wYIjIYd4XQh8tgqbMUwKBgQCcF5vtIsoNAnRZD82tXOiSulg4F3oKUaQgL642
yg9d95Dynot0e3mtyg9ojvz6rT3UPpS+pFH06IwrKt026wYFt/rUefpE7+vOLMsm
MhsPYdUIHRuItwxWNBv+2EWpTnUkPx9BReRgLYDEj9hVDtXU9uVkG8aA6Fhdr5Zt
M+fwQQKBgAbcuUeR9RseENAYl2fTf6ZaZ6pbK2HHo3qOqaY5vWpMESUOsAuC46qj
anwM76TcEbTBHgEWMDYiAYURXPVXisouoD6jsTcMDErwM/3kqMQ7rD6VM9uK7UF+
M0dV7SSA2lMvENr54k6V7zdaxnRDu8GL+OHtiZxeBG1P4pKhvf9l
-----END RSA PRIVATE KEY-----`

const csrConf = `[req]
prompt = no
distinguished_name = dn
req_extensions = req_ext

[dn]
CN = AoS message-handler
O = EPAM
OU = AoS

[req_ext]
basicConstraints = CA:false
subjectKeyIdentifier=hash

[ext]
# PKIX recommendation.
basicConstraints = CA:false
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid:always
keyUsage = digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth,clientAuth
#subjectAltName = @alt_names

[alt_names]
DNS.1 = *.westeurope.cloudapp.azure.com
DNS.2 = *.kyiv.epam.com
DNS.3 = *.minsk.epam.com
DNS.4 = *.aos-dev.test
DNS.5 = rabbitmq
DNS.6 = localhost
IP.1 = 127.0.0.1`

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

// GetCACertificate returns CA certificate.
func GetCACertificate() (cert []byte) {
	return []byte(caCertificate)
}

// GetCAKey returns CA key.
func GetCAKey() (cert []byte) {
	return []byte(caKey)
}

// CreateCertificate creates certificate.
func CreateCertificate(workDir string, csr []byte) (cert []byte, err error) {
	tmpDir, err := ioutil.TempDir(workDir, "cert*")
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	defer os.RemoveAll(tmpDir)

	caCertFile := path.Join(tmpDir, "ca.crt")
	caKeyFile := path.Join(tmpDir, "ca.key")
	csrFile := path.Join(tmpDir, "unit.csr")
	csrConfFile := path.Join(tmpDir, "csr.conf")
	unitCertFile := path.Join(tmpDir, "unit.der")

	if err = ioutil.WriteFile(csrFile, csr, filePerm); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(caCertFile, GetCACertificate(), filePerm); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(caKeyFile, GetCAKey(), filePerm); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(csrConfFile, []byte(csrConf), filePerm); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	var out []byte

	if out, err = exec.Command("openssl", "req", "-inform", "PEM", "-in",
		csrFile, "-out", csrFile+".pem").CombinedOutput(); err != nil {
		return nil, aoserrors.Errorf("message: %s, %s", string(out), err)
	}

	if out, err = exec.Command("openssl", "x509", "-req", "-in", csrFile+".pem",
		"-CA", caCertFile, "-CAkey", caKeyFile, "-extfile", csrConfFile, "-extensions", "ext",
		"-outform", "PEM", "-out", unitCertFile, "-CAcreateserial", "-days", "3650").CombinedOutput(); err != nil {
		return nil, aoserrors.Errorf("message: %s, %s", string(out), err)
	}

	certData, err := ioutil.ReadFile(unitCertFile)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	caData, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	certData = append(certData, caData...)
	certData = append(certData, '\n')

	return certData, nil
}
