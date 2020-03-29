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
	"path"
	"path/filepath"
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
	operationState []byte
	moduleStatuses map[string]error
}

type testModule struct {
	id     string
	status error
}

type testPlatformController struct {
	version    uint64
	platformID string
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var tmpDir string

var updater *updatehandler.Handler

var cfg config.Config

var modules = []updatehandler.UpdateModule{
	&testModule{id: "id1"},
	&testModule{id: "id2"},
	&testModule{id: "id3"},
}

var storage = testStorage{
	operationState: []byte("{}"),
	moduleStatuses: make(map[string]error)}

var platform = testPlatformController{}

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

	cfg = config.Config{UpgradeDir: tmpDir}

	if updater, err = updatehandler.New(&cfg, modules, &platform, &storage); err != nil {
		log.Fatalf("Can't create updater: %s", err)
	}

	ret := m.Run()

	updater.Close()

	if err := os.RemoveAll(tmpDir); err != nil {
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

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))

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

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))

	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	module, _ := modules[1].(*testModule)
	module.status = errors.New("upgrade error")
	defer func() { module.status = nil }()

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

func TestUpgradeBadVersion(t *testing.T) {
	version := updater.GetCurrentVersion() + 1

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	if err := updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Error("Wait operation timeout")

	case status := <-updater.StatusChannel():
		if status != umprotocol.SuccessStatus {
			t.Fatalf("Upgrade failed: %s", updater.GetLastError())
		}
	}

	if updater.GetCurrentVersion() != version {
		t.Error("Wrong current version")
	}

	initialVersion := updater.GetCurrentVersion()

	version--

	// When a new bundle version less than current
	if err := updater.Upgrade(version, imageInfo); err == nil {
		t.Fatal("Error expected, but a new version less than a current")
	}

	if updater.GetCurrentVersion() != initialVersion {
		t.Error("Wrong current version")
	}

	// When a new bundle version equals than current
	version = initialVersion
	if err := updater.Upgrade(version, imageInfo); err == nil {
		t.Fatal("Error expected, but a new version equals than a current")
	}

	if updater.GetCurrentVersion() != initialVersion {
		t.Error("Wrong current version")
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

	if err = os.MkdirAll(path.Join(tmpDir, "image"), 0755); err != nil {
		return imageInfo, err
	}

	if err := ioutil.WriteFile(path.Join(tmpDir, "image", "metadata.json"), []byte(metadataJSON), 0644); err != nil {
		return imageInfo, err
	}

	if err := exec.Command("tar", "-C", path.Join(tmpDir, "image"), "-czf", imagePath, "./").Run(); err != nil {
		return imageInfo, err
	}

	fileInfo, err := image.CreateFileInfo(imagePath)
	if err != nil {
		return imageInfo, err
	}

	imageInfo.Path = filepath.Base(imagePath)
	imageInfo.Sha256 = fileInfo.Sha256
	imageInfo.Sha512 = fileInfo.Sha512
	imageInfo.Size = fileInfo.Size

	return imageInfo, nil
}

func (storage *testStorage) SetOperationState(state []byte) (err error) {
	storage.operationState = state
	return nil
}

func (storage *testStorage) GetOperationState() (state []byte, err error) {
	return storage.operationState, nil
}

func (module *testModule) GetID() (id string) {
	return module.id
}

func (module *testModule) Init() (err error) {
	return nil
}

func (module *testModule) Upgrade(version uint64, path string) (rebootRequired bool, err error) {
	return false, module.status
}

func (module *testModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

func (module *testModule) FinishUpgrade(version uint64) (err error) {
	return nil
}

func (module *testModule) Revert(version uint64) (rebootRequired bool, err error) {
	return false, module.status
}

func (module *testModule) CancelRevert(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

func (module *testModule) FinishRevert(version uint64) (err error) {
	return nil
}

func (module *testModule) Close() (err error) {
	return nil
}

func (platform *testPlatformController) GetVersion() (version uint64, err error) {
	return platform.version, nil
}

func (platform *testPlatformController) SetVersion(version uint64) (err error) {
	platform.version = version

	return nil
}

func (platform *testPlatformController) GetPlatformID() (id string, err error) {
	return "SomePlatform", nil
}
