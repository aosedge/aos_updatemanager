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

package filemodule_test

import (
	"compress/gzip"
	"io/ioutil"
	"os"
	"path"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/filemodule"
)

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

var stateStorage = testStorage{state: []byte("")}

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

	if err = os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}

	configJSON := `
		[{
			"id": "testConfig",
			"path": "tmp/test_configuration.cfg"
		}]
	`

	module, err = filemodule.New("TestFileUpdate", []byte(configJSON), &stateStorage)
	if err != nil {
		log.Fatalf("Can't create file update module module: %s", err)
	}

	ret := m.Run()

	module.Close()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id := module.GetID()
	if id != "TestFileUpdate" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestUpgrade(t *testing.T) {
	var updatedString string = "This updated file"
	if err := ioutil.WriteFile("tmp/test_configuration.cfg", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	tmpDir, err := ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}
	defer os.Remove(tmpDir)

	metadataJSON := `
	{
		"componentType": "TestFileUpdate",
		"resources": [{
			"id": "testConfig",
			"resource": "testConfig.gz"
		}]
	}
	`

	if err := ioutil.WriteFile(path.Join(tmpDir, "metadata.json"), []byte(metadataJSON), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	file, err := os.Create(path.Join(tmpDir, "testConfig.gz"))
	if err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	zw := gzip.NewWriter(file)
	zw.Write([]byte(updatedString))
	zw.Close()

	file.Close()

	if _, err := module.Upgrade(0, tmpDir); err != nil {
		t.Errorf("Upgrade failed: %s", err)
	}

	resultData, err := ioutil.ReadFile("tmp/test_configuration.cfg")
	if err != nil {
		t.Error("Can't read tmp/test_configuration.cfg ", err)
	}

	if string(resultData) != updatedString {
		t.Error("Incorrect update content")
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}
}

func TestWrongJson(t *testing.T) {
	configJSON := `{
		Wrong json format
		]
	}`

	module, err := filemodule.New("TestComponent", []byte(configJSON), &stateStorage)
	if err == nil {
		module.Close()
		log.Fatalf("Expecting error here")
	}
}

func (storage *testStorage) GetModuleState(id string) (state []byte, err error) {
	return storage.state, nil
}

func (storage *testStorage) SetModuleState(id string, state []byte) (err error) {
	storage.state = state

	return nil
}
func (storage *testStorage) GetControllerState(controllerID string, name string) (value []byte, err error) {
	return []byte("valid"), nil
}

func (storage *testStorage) SetControllerState(controllerID string, name string, value []byte) (err error) {
	return nil
}

func (storage *testStorage) SetOperationState(state []byte) (err error) {

	return nil
}

func (storage *testStorage) GetOperationState() (state []byte, err error) {
	return []byte("valid"), nil
}

func (storage *testStorage) GetSystemVersion() (version uint64, err error) {
	return 1, nil
}

func (storage *testStorage) SetSystemVersion(version uint64) (err error) {
	return nil
}
