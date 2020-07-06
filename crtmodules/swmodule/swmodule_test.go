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

package swmodule_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/crthandler"
	"aos_updatemanager/crtmodules/swmodule"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type crtDesc struct {
	crtType string
	crtInfo crthandler.CrtInfo
}

type testStorage struct {
	crts []crtDesc
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var tmpDir string

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing temporary dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestUpdateCertificate(t *testing.T) {
	crtStorage := path.Join(tmpDir, "crtStorage")

	if err := os.RemoveAll(crtStorage); err != nil {
		t.Fatalf("Can't remove cert storage: %s", err)
	}

	config := json.RawMessage(fmt.Sprintf(`{"storagePath":"%s","maxItems":%d}`, crtStorage, 1))

	storage := &testStorage{}

	module, err := swmodule.New("test", config, storage)
	if err != nil {
		t.Fatalf("Can't create SW module: %s", err)
	}
	defer module.Close()

	// Create keys

	csr, err := module.CreateKeys("testsystem", "")
	if err != nil {
		t.Fatalf("Can't create keys: %s", err)
	}

	// Verify CSR

	csrFile := path.Join(tmpDir, "data.csr")

	if err = ioutil.WriteFile(csrFile, []byte(csr), 0644); err != nil {
		t.Fatalf("Can't write CSR to file: %s", err)
	}

	out, err := exec.Command("openssl", "req", "-text", "-noout", "-verify", "-inform", "PEM", "-in", csrFile).CombinedOutput()
	if err != nil {
		t.Errorf("Can't verify CSR: %s, %s", out, err)
	}

	// Apply certificate

	crt, err := generateCertificate(csr)
	if err != nil {
		t.Fatalf("Can't generate certificate: %s", err)
	}

	crtURL, keyURL, err := module.ApplyCertificate(crt)
	if err != nil {
		t.Fatalf("Can't apply certificate: %s", err)
	}

	// Get certificate

	block, _ := pem.Decode([]byte(crt))

	x509Crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Can't parse certificate: %s", err)
	}

	crtInfo, err := storage.GetCertificate(base64.StdEncoding.EncodeToString(x509Crt.RawIssuer), fmt.Sprintf("%X", x509Crt.SerialNumber))
	if err != nil {
		t.Fatalf("Can't get certificate: %s", err)
	}

	if crtURL != crtInfo.CrtURL || keyURL != crtInfo.KeyURL {
		t.Errorf("Wrong certificate or key URL: %s, %s", crtInfo.CrtURL, crtInfo.KeyURL)
	}

	// Check encrypt/decrypt with private key

	key, err := getKey(keyURL)
	if err != nil {
		t.Fatalf("Can't get key: %s", err)
	}

	originMessage := []byte("This is origin message")

	encryptedData, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, originMessage)
	if err != nil {
		t.Fatalf("Can't encrypt message: %s", err)
	}

	decryptedData, err := rsa.DecryptPKCS1v15(rand.Reader, key, encryptedData)
	if err != nil {
		t.Fatalf("Can't decrypt message: %s", err)
	}

	if !bytes.Equal(originMessage, decryptedData) {
		t.Error("Decrypt error")
	}
}

func TestMaxItems(t *testing.T) {
	crtStorage := path.Join(tmpDir, "crtStorage")

	if err := os.RemoveAll(crtStorage); err != nil {
		t.Fatalf("Can't remove cert storage: %s", err)
	}

	config := json.RawMessage(fmt.Sprintf(`{"storagePath":"%s","maxItems":%d}`, crtStorage, 1))

	storage := &testStorage{}

	module, err := swmodule.New("test", config, storage)
	if err != nil {
		t.Fatalf("Can't create SW module: %s", err)
	}
	defer module.Close()

	for i := 0; i < 3; i++ {

		// Create keys

		csr, err := module.CreateKeys("testsystem", "")
		if err != nil {
			t.Fatalf("Can't create keys: %s", err)
		}

		// Apply certificate

		crt, err := generateCertificate(csr)
		if err != nil {
			t.Fatalf("Can't generate certificate: %s", err)
		}

		crtURL, keyURL, err := module.ApplyCertificate(crt)
		if err != nil {
			t.Fatalf("Can't apply certificate: %s", err)
		}

		// Check key files

		keyFiles, err := getKeyFiles(crtStorage)
		if err != nil {
			t.Fatalf("Can't get key files")
		}

		if len(keyFiles) != 1 {
			t.Fatalf("Wrong key files count: %d", len(keyFiles))
		}

		if err = checkFileURL(keyURL, keyFiles[0]); err != nil {
			t.Errorf("Check key URL error: %s", err)
		}

		// Check cert files

		crtFiles, err := getCrtFiles(crtStorage)
		if err != nil {
			t.Fatalf("Can't get cert files")
		}

		if len(crtFiles) != 1 {
			t.Fatalf("Wrong cert files count: %d", len(crtFiles))
		}

		if err = checkFileURL(crtURL, crtFiles[0]); err != nil {
			t.Errorf("Check cert URL error: %s", err)
		}
	}
}

func TestSyncStorage(t *testing.T) {
	// Test items:
	// * valid     - crt file, handle, DB entry
	// * wrongDB   - DB entry but no crt file
	// * wrongFile - no DB entry and no handle
	// If crt file has DB entry but no handle - not considered

	testData := []string{"valid", "wrongDB", "wrongDB", "valid", "wrongFile", "validFile", "valid", "valid"}

	var goodItems []crthandler.CrtInfo

	crtStorage := path.Join(tmpDir, "crtStorage")

	if err := os.RemoveAll(crtStorage); err != nil {
		t.Fatalf("Can't remove cert storage: %s", err)
	}

	config := json.RawMessage(fmt.Sprintf(`{"storagePath":"%s","maxItems":%d}`, crtStorage, len(testData)))

	storage := &testStorage{}

	module, err := swmodule.New("test", config, storage)
	if err != nil {
		t.Fatalf("Can't create SW module: %s", err)
	}

	for _, item := range testData {
		// Create keys

		csr, err := module.CreateKeys("testsystem", "")
		if err != nil {
			t.Fatalf("Can't create keys: %s", err)
		}

		// Apply certificate

		crt, err := generateCertificate(csr)
		if err != nil {
			t.Fatalf("Can't generate certificate: %s", err)
		}

		crtURL, keyURL, err := module.ApplyCertificate(crt)
		if err != nil {
			t.Fatalf("Can't apply certificate: %s", err)
		}

		crtVal, err := url.Parse(crtURL)
		if err != nil {
			t.Errorf("Can't pase crt URL: %s", err)
		}

		keyVal, err := url.Parse(keyURL)
		if err != nil {
			t.Errorf("Can't parse key URL: %s", err)
		}

		crtFile := crtVal.Path
		keyFile := keyVal.Path

		switch item {
		case "wrongDB":
			if err = os.Remove(crtFile); err != nil {
				t.Errorf("Can't remove crt file: %s", err)
			}

			if err = os.Remove(keyFile); err != nil {
				t.Errorf("Can't remove key file: %s", err)
			}

		case "wrongFile":
			if err = storage.RemoveCertificate("test", crtURL); err != nil {
				t.Errorf("Can't remove crt entry: %s", err)
			}

			if err = os.Remove(keyFile); err != nil {
				t.Errorf("Can't remove key file: %s", err)
			}

		case "validFile":
			if err = storage.RemoveCertificate("test", crtURL); err != nil {
				t.Errorf("Can't remove crt entry: %s", err)
			}

			fallthrough

		default:
			goodItems = append(goodItems, crthandler.CrtInfo{CrtURL: crtURL, KeyURL: keyURL})
		}
	}

	module.Close()

	if module, err = swmodule.New("test", config, storage); err != nil {
		t.Fatalf("Can't create SW module: %s", err)
	}
	defer module.Close()

	if err = module.SyncStorage(); err != nil {
		t.Fatalf("Can't sync storage: %s", err)
	}

	crtInfos, err := storage.GetCertificates("test")
	if err != nil {
		t.Fatalf("Can't get certificates: %s", err)
	}

	for _, goodItem := range goodItems {
		found := false

		infoIndex := 0

		for i, info := range crtInfos {
			if info.CrtURL == goodItem.CrtURL && info.KeyURL == goodItem.KeyURL {
				found = true
				infoIndex = i
			}
		}

		if !found {
			t.Errorf("Expected item not found in storage, crtURL: %s, keyURL: %s", goodItem.CrtURL, goodItem.KeyURL)
		} else {
			crtInfos = append(crtInfos[:infoIndex], crtInfos[infoIndex+1:]...)
		}
	}

	for _, badItem := range crtInfos {
		t.Errorf("Item should not be in srorage, crtURL: %s, keyURL: %s", badItem.CrtURL, badItem.KeyURL)
	}
}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

func (storage *testStorage) AddCertificate(crtType string, crt crthandler.CrtInfo) (err error) {
	for _, item := range storage.crts {
		if item.crtInfo.Issuer == crt.Issuer && item.crtInfo.Serial == crt.Serial {
			return errors.New("certificate already exists")
		}
	}

	storage.crts = append(storage.crts, crtDesc{crtType, crt})

	return nil
}

func (storage *testStorage) GetCertificate(issuer, serial string) (crt crthandler.CrtInfo, err error) {
	for _, item := range storage.crts {
		if item.crtInfo.Issuer == issuer && item.crtInfo.Serial == serial {
			return item.crtInfo, nil
		}
	}

	return crt, errors.New("certificate not found")
}

func (storage *testStorage) GetCertificates(crtType string) (crts []crthandler.CrtInfo, err error) {
	for _, item := range storage.crts {
		if item.crtType == crtType {
			crts = append(crts, item.crtInfo)
		}
	}

	return crts, nil
}

func (storage *testStorage) RemoveCertificate(crtType, crtURL string) (err error) {
	for i, item := range storage.crts {
		if item.crtType == crtType && item.crtInfo.CrtURL == crtURL {
			storage.crts = append(storage.crts[:i], storage.crts[i+1:]...)

			return nil
		}
	}

	return errors.New("certificate not found")
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func generateCertificate(csr string) (crt string, err error) {
	caCrt :=
		`-----BEGIN CERTIFICATE-----
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

	caKey :=
		`-----BEGIN RSA PRIVATE KEY-----
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

	csrConf :=
		`[req]
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

	caCrtFile := path.Join(tmpDir, "ca.crt")
	caKeyFile := path.Join(tmpDir, "ca.key")
	csrFile := path.Join(tmpDir, "unit.csr")
	csrConfFile := path.Join(tmpDir, "csr.conf")
	unitCrtFile := path.Join(tmpDir, "unit.der")

	if err = ioutil.WriteFile(csrFile, []byte(csr), 0644); err != nil {
		return "", err
	}

	if err = ioutil.WriteFile(caCrtFile, []byte(caCrt), 0644); err != nil {
		return "", err
	}

	if err = ioutil.WriteFile(caKeyFile, []byte(caKey), 0644); err != nil {
		return "", err
	}

	if err = ioutil.WriteFile(csrConfFile, []byte(csrConf), 0644); err != nil {
		return "", err
	}

	var out []byte

	if out, err = exec.Command("openssl", "req", "-inform", "PEM", "-in", csrFile, "-out", csrFile+".pem").CombinedOutput(); err != nil {
		return "", fmt.Errorf("message: %s, %s", string(out), err)
	}

	if out, err = exec.Command("openssl", "x509", "-req", "-in", csrFile+".pem",
		"-CA", caCrtFile, "-CAkey", caKeyFile, "-extfile", csrConfFile, "-extensions", "ext",
		"-outform", "PEM", "-out", unitCrtFile, "-CAcreateserial", "-days", "3650").CombinedOutput(); err != nil {
		return "", fmt.Errorf("message: %s, %s", string(out), err)
	}

	crtData, err := ioutil.ReadFile(unitCrtFile)
	if err != nil {
		return "", err
	}

	return string(crtData), nil
}

func getKey(keyURL string) (key *rsa.PrivateKey, err error) {
	keyVal, err := url.Parse(keyURL)
	if err != nil {
		return nil, err
	}

	keyData, err := ioutil.ReadFile(keyVal.Path)
	if err != nil {
		return nil, err
	}

	keyPem, _ := pem.Decode(keyData)
	if err != nil {
		return nil, err
	}

	return x509.ParsePKCS1PrivateKey(keyPem.Bytes)
}

func checkFileURL(strURL string, file string) (err error) {
	valURL, err := url.Parse(strURL)
	if err != nil {
		return err
	}

	if file != valURL.Path {
		return fmt.Errorf("cert file mismatch: %s !=%s", strURL, file)
	}

	return nil
}

func getKeyFiles(storagePath string) (files []string, err error) {
	content, err := ioutil.ReadDir(storagePath)
	if err != nil {
		return nil, err
	}

	for _, item := range content {
		if item.IsDir() {
			continue
		}

		if path.Ext(item.Name()) != ".key" {
			continue
		}

		files = append(files, path.Join(storagePath, item.Name()))
	}

	return files, nil
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

		if path.Ext(item.Name()) != ".crt" {
			continue
		}

		files = append(files, path.Join(storagePath, item.Name()))
	}

	return files, nil
}
