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

package crthandler_test

import (
	"aos_updatemanager/config"
	"aos_updatemanager/crthandler"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type moduleData struct {
	csr    string
	crtURL string
}

type testModule struct {
	data *moduleData
}

type crtDesc struct {
	crtType string
	crtInfo crthandler.CrtInfo
}

type testStorage struct {
	crts []crtDesc
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var tmpDir string

var cfg config.Config

var modules map[string]*testModule

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
	cfg = config.Config{
		CrtModules: []config.ModuleConfig{
			config.ModuleConfig{ID: "crt1", Plugin: "testmodule"},
			config.ModuleConfig{ID: "crt2", Plugin: "testmodule"},
			config.ModuleConfig{ID: "crt3", Plugin: "testmodule"}}}

	modules = make(map[string]*testModule)

	crthandler.RegisterPlugin("testmodule", func(crtType string, configJSON json.RawMessage,
		storage crthandler.CrtStorage) (module crthandler.CrtModule, err error) {
		crtModule := &testModule{data: &moduleData{}}

		modules[crtType] = crtModule

		return crtModule, nil
	})
	ret := m.Run()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestCreateKeys(t *testing.T) {
	storage := &testStorage{}

	handler, err := crthandler.New(&cfg, storage)
	if err != nil {
		t.Fatalf("Can't create crt handler: %s", err)
	}
	defer handler.Close()

	modules["crt1"].data.csr = "this is csr"

	csr, err := handler.CreateKeys("crt1", "systemID", "password")
	if err != nil {
		t.Fatalf("Can't create keys: %s", err)
	}

	if modules["crt1"].data.csr != csr {
		t.Errorf("Wrong CSR value: %s", string(csr))
	}
}

func TestApplyCertificate(t *testing.T) {
	storage := &testStorage{}

	handler, err := crthandler.New(&cfg, storage)
	if err != nil {
		t.Fatalf("Can't create crt handler: %s", err)
	}
	defer handler.Close()

	modules["crt2"].data.crtURL = "crtURL"

	crtURL, err := handler.ApplyCertificate("crt2", "this is certificate")
	if err != nil {
		t.Fatalf("Can't apply certificate: %s", err)
	}

	if modules["crt2"].data.crtURL != crtURL {
		t.Errorf("Wrong crt URL: %s", crtURL)
	}
}

func TestGetCertificate(t *testing.T) {
	storage := &testStorage{}

	storage.AddCertificate("crt1", crthandler.CrtInfo{base64.StdEncoding.EncodeToString([]byte("issuer")), "1", "crtURL1", "keyURL1", time.Now()})
	storage.AddCertificate("crt1", crthandler.CrtInfo{base64.StdEncoding.EncodeToString([]byte("issuer")), "2", "crtURL2", "keyURL2", time.Now()})
	storage.AddCertificate("crt1", crthandler.CrtInfo{base64.StdEncoding.EncodeToString([]byte("issuer")), "3", "crtURL3", "keyURL3", time.Now()})

	handler, err := crthandler.New(&cfg, storage)
	if err != nil {
		t.Fatalf("Can't create crt handler: %s", err)
	}
	defer handler.Close()

	crtURL, keyURL, err := handler.GetCertificate("crt1", []byte("issuer"), "2")
	if err != nil {
		t.Fatalf("Can't get certificate: %s", err)
	}

	if crtURL != "crtURL2" {
		t.Errorf("Wrong crt URL: %s", crtURL)
	}

	if keyURL != "keyURL2" {
		t.Errorf("Wrong key URL: %s", keyURL)
	}

	if crtURL, keyURL, err = handler.GetCertificate("crt1", nil, ""); err != nil {
		t.Fatalf("Can't get certificate: %s", err)
	}

	if crtURL != "crtURL1" {
		t.Errorf("Wrong crt URL: %s", crtURL)
	}

	if keyURL != "keyURL1" {
		t.Errorf("Wrong key URL: %s", keyURL)
	}
}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

func (module *testModule) SyncStorage() (err error) {
	return nil
}

func (module *testModule) CreateKeys(systemID, password string) (csr string, err error) {
	return module.data.csr, nil
}

func (module *testModule) ApplyCertificate(crt string) (crtURL, keyURL string, err error) {
	return module.data.crtURL, "", nil
}

func (module *testModule) Close() (err error) {
	return nil
}

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
