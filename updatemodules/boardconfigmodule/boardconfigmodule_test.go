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

package boardconfigmodule_test

import (
	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/boardconfigmodule"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/aoscloud/aos_common/aoserrors"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const configTemplate = `
 {
	 "vendorVersion": "%s",
	 "resources": [{
		 "id": "testConfig",
		 "resource": "some_old_resources"
	 }]
 }
 `

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	state []byte
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var module updatehandler.UpdateModule

var (
	boardFileName string
	tmpDir        string
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
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

	boardFileName = path.Join(tmpDir, "test_configuration.cfg")

	module, err = boardconfigmodule.New("boardConfig",
		[]byte(fmt.Sprintf(`{"path": "%s"}`, boardFileName)), &testStorage{}, nil)
	if err != nil {
		log.Fatalf("Can't create module: %s", err)
	}

	module.Close()

	ret := m.Run()

	if err = os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error deleting temporary dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id := module.GetID()
	if id != "boardConfig" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestGetVersion(t *testing.T) {
	if _, err := createBoardConfig("v1.0"); err != nil {
		t.Fatalf("Can't create board config: %s", err)
	}

	if err := module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	version, err := module.GetVendorVersion()
	if err != nil {
		log.Fatalf("Can't get board config vendor version: %s", err)
	}

	if version != "v1.0" {
		t.Errorf("Wrong board config vendor version: %s", version)
	}
}

func TestUpdate(t *testing.T) {
	if _, err := createBoardConfig("v1.0"); err != nil {
		t.Fatalf("Can't create board config: %s", err)
	}

	imagePath := path.Join(tmpDir, "image.gz")

	configPayload, err := createBoardImage(imagePath, "v2.0")
	if err != nil {
		t.Fatalf("Can't create board image: %s", err)
	}

	if err := module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	if err := module.Prepare(imagePath, "v2.0", nil); err != nil {
		t.Errorf("Prepare failed: %s", err)
	}

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Update failed: %s", err)
	}

	if !rebootRequired {
		t.Error("Reboot is required")
	}

	if err = module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	if rebootRequired, err = module.Apply(); err != nil {
		t.Errorf("Apply failed: %s", err)
	}

	if rebootRequired {
		t.Error("Reboot is not required")
	}

	version, err := module.GetVendorVersion()
	if err != nil {
		log.Fatalf("Can't get board vendor version: %s", err)
	}

	if version != "v2.0" {
		t.Errorf("Wrong board config version after update: %s", version)
	}

	currentPayload, err := ioutil.ReadFile(boardFileName)
	if err != nil {
		t.Error("Can't read tmp/test_configuration.cfg ", err)
	}

	if string(currentPayload) != configPayload {
		t.Error("Incorrect update content")
	}
}

func TestRevert(t *testing.T) {
	configPayload, err := createBoardConfig("v2.0")
	if err != nil {
		t.Fatalf("Can't create board config: %s", err)
	}

	imagePath := path.Join(tmpDir, "image.gz")

	if _, err := createBoardImage(imagePath, "v3.0"); err != nil {
		t.Fatalf("Can't create board image: %s", err)
	}

	if err := module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	if err := module.Prepare(imagePath, "v3.0", nil); err != nil {
		t.Errorf("Prepare failed: %s", err)
	}

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Update failed: %s", err)
	}

	if !rebootRequired {
		t.Error("Reboot is required")
	}

	if err = module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	if rebootRequired, err = module.Revert(); err != nil {
		t.Errorf("Apply failed: %s", err)
	}

	if !rebootRequired {
		t.Error("Reboot is required")
	}

	if err := module.Init(); err != nil {
		t.Errorf("Init failed: %s", err)
	}

	version, err := module.GetVendorVersion()
	if err != nil {
		log.Fatalf("Can't get board vendor version: %s", err)
	}

	if version != "v2.0" {
		t.Errorf("Wrong board config version after update: %s", version)
	}

	currentPayload, err := ioutil.ReadFile(boardFileName)
	if err != nil {
		t.Error("Can't read tmp/test_configuration.cfg ", err)
	}

	if string(currentPayload) != configPayload {
		t.Error("Incorrect update content")
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (storage *testStorage) GetModuleState(id string) (state []byte, err error) {
	return storage.state, nil
}

func (storage *testStorage) SetModuleState(id string, state []byte) (err error) {
	storage.state = state

	return nil
}

func createBoardConfig(version string) (payload string, err error) {
	payload = fmt.Sprintf(configTemplate, version)

	if err = ioutil.WriteFile(boardFileName, []byte(payload), 0o600); err != nil {
		return "", aoserrors.Wrap(err)
	}

	return payload, nil
}

func createBoardImage(imagePath string, version string) (payload string, err error) {
	file, err := os.Create(imagePath)
	if err != nil {
		return "", aoserrors.Wrap(err)
	}
	defer file.Close()

	zw := gzip.NewWriter(file)
	defer zw.Close()

	payload = fmt.Sprintf(configTemplate, version)

	if _, err := zw.Write([]byte(payload)); err != nil {
		return "", aoserrors.Wrap(err)
	}

	return payload, nil
}
