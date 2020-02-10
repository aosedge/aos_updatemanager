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

package updatehandler_test

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/image"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"

	"aos_updatemanager/config"
	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	state            int
	operationStage   int
	imagePath        string
	operationVersion uint64
	currentVersion   uint64
	lastError        error

	moduleStatuses map[string]error
}

type testModuleProvider struct {
	modules map[string]interface{}
}

type testModule struct {
	status error
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var updater *updatehandler.Handler

var moduleProvider = testModuleProvider{
	modules: map[string]interface{}{
		"id1": &testModule{},
		"id2": &testModule{},
		"id3": &testModule{},
	},
}

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
		log.Fatalf("Can't crate tmp dir: %s", err)
	}

	if updater, err = updatehandler.New(&config.Config{UpgradeDir: "tmp"}, &moduleProvider, nil, &testStorage{moduleStatuses: make(map[string]error)}); err != nil {
		log.Fatalf("Can't create updater: %s", err)
	}

	ret := m.Run()

	updater.Close()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetCurrentVersion(t *testing.T) {
	version := updater.GetCurrentVersion()

	if version != 0 {
		t.Errorf("Wrong version: %d", version)
	}
}

func TestUpgradeRevert(t *testing.T) {
	version := updater.GetCurrentVersion()

	version++

	imageInfo, err := createImage("tmp/testimage.bin")

	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	if err := updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Error("wait operation timeout")

	case status := <-updater.StatusChannel():
		if status != umprotocol.SuccessStatus {
			t.Fatalf("Upgrade failed: %s", updater.GetLastError())
		}
	}

	if updater.GetCurrentVersion() != version {
		t.Error("Wrong current version")
	}

	if updater.GetOperationVersion() != version {
		t.Error("Wrong operation version")
	}

	if updater.GetLastOperation() != umprotocol.UpgradeOperation {
		t.Error("Wrong operation")
	}

	if updater.GetStatus() != umprotocol.SuccessStatus {
		t.Error("Wrong status")
	}

	if err := updater.GetLastError(); err != nil {
		t.Errorf("Upgrade error: %s", err)
	}

	version--

	if err := updater.Revert(version); err != nil {
		t.Errorf("Upgrading error: %s", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Error("wait operation timeout")

	case <-updater.StatusChannel():
	}

	if updater.GetCurrentVersion() != version {
		t.Error("Wrong current version")
	}

	if updater.GetOperationVersion() != version {
		t.Error("Wrong operation version")
	}

	if updater.GetLastOperation() != umprotocol.RevertOperation {
		t.Error("Wrong operation")
	}

	if updater.GetStatus() != umprotocol.SuccessStatus {
		t.Error("Wrong status")
	}

	if err := updater.GetLastError(); err != nil {
		t.Errorf("Upgrade error: %s", err)
	}
}

func TestUpgradeFailed(t *testing.T) {
	version := updater.GetCurrentVersion()

	imageInfo, err := createImage("tmp/testimage.bin")

	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	module, err := getTestModule("id2")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	module.status = errors.New("upgrade error")

	if err := updater.Upgrade(version+1, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Error("wait operation timeout")

	case status := <-updater.StatusChannel():
		if status != umprotocol.FailedStatus {
			t.Fatal("Upgrade should fails")
		}
	}

	if updater.GetCurrentVersion() != version {
		t.Error("Wrong current version")
	}

	if updater.GetOperationVersion() != version+1 {
		t.Error("Wrong operation version")
	}

	if updater.GetLastOperation() != umprotocol.UpgradeOperation {
		t.Error("Wrong operation")
	}

	if updater.GetStatus() != umprotocol.FailedStatus {
		t.Error("Wrong status")
	}

	if err := updater.GetLastError(); err == nil {
		t.Error("Wrong last error")
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createImage(imagePath string) (imageInfo umprotocol.ImageInfo, err error) {
	metadataJSON := `
{
    "platformId": "SomePlatform",
    "bundleVersion": "v1.24 28-02-2020",
    "bundleDescription": "This is super update",
    "updateItems": [
        {
            "type": "id1",
            "path": "id1Dir"
        },
        {
            "type": "id2",
            "path": "id2Dir"
        },
        {
            "type": "id3",
            "path": "id3Dir"
        }
    ]
}`

	if err = os.MkdirAll("tmp/image", 0755); err != nil {
		return imageInfo, err
	}

	if err := ioutil.WriteFile("tmp/image/metadata.json", []byte(metadataJSON), 0644); err != nil {
		return imageInfo, err
	}

	if err := exec.Command("tar", "-C", "tmp/image", "-czf", imagePath, "./").Run(); err != nil {
		return imageInfo, err
	}

	fileInfo, err := image.CreateFileInfo(imagePath)
	if err != nil {
		return imageInfo, err
	}

	imageInfo.Path = imagePath
	imageInfo.Sha256 = fileInfo.Sha256
	imageInfo.Sha512 = fileInfo.Sha512
	imageInfo.Size = fileInfo.Size

	return imageInfo, nil
}

func (storage *testStorage) SetState(state int) (err error) {
	storage.state = state
	return nil
}

func (storage *testStorage) GetState() (state int, err error) {
	return storage.state, nil
}

func (storage *testStorage) SetOperationStage(stage int) (err error) {
	storage.operationStage = stage
	return nil
}

func (storage *testStorage) GetOperationStage() (stage int, err error) {
	return storage.operationStage, nil
}

func (storage *testStorage) SetImagePath(path string) (err error) {
	storage.imagePath = path
	return nil
}

func (storage *testStorage) GetImagePath() (path string, err error) {
	return storage.imagePath, nil
}

func (storage *testStorage) SetCurrentVersion(version uint64) (err error) {
	storage.currentVersion = version
	return nil
}

func (storage *testStorage) GetCurrentVersion() (version uint64, err error) {
	return storage.currentVersion, nil
}

func (storage *testStorage) SetOperationVersion(version uint64) (err error) {
	storage.operationVersion = version
	return nil
}

func (storage *testStorage) GetOperationVersion() (version uint64, err error) {
	return storage.operationVersion, nil
}

func (storage *testStorage) SetLastError(lastError error) (err error) {
	storage.lastError = lastError
	return nil
}

func (storage *testStorage) GetLastError() (lastError error, err error) {
	return storage.lastError, nil
}

func (storage *testStorage) AddModuleStatus(id string, status error) (err error) {
	storage.moduleStatuses[id] = status

	return nil
}

func (storage *testStorage) RemoveModuleStatus(id string) (err error) {
	delete(storage.moduleStatuses, id)

	return nil
}

func (storage *testStorage) GetModuleStatuses() (moduleStatuses map[string]error, err error) {
	return storage.moduleStatuses, nil
}

func (storage *testStorage) ClearModuleStatuses() (err error) {
	storage.moduleStatuses = make(map[string]error)

	return nil
}

func (module *testModule) GetID() (id string) {
	return ""
}

func (module *testModule) Upgrade(path string) (err error) {
	return module.status
}

func (module *testModule) Revert() (err error) {
	return module.status
}

func (provider *testModuleProvider) GetModuleByID(id string) (module interface{}, err error) {
	testModule, ok := provider.modules[id]
	if !ok {
		return nil, errors.New("module not found")
	}

	return testModule, nil
}

func getTestModule(id string) (module *testModule, err error) {
	m, err := moduleProvider.GetModuleByID(id)
	if err != nil {
		return nil, err
	}

	var ok bool

	if module, ok = m.(*testModule); !ok {
		return nil, errors.New("wrong module type")
	}

	return module, nil
}
