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
	id             string
	status         error
	rebootRequired bool
}

type testPlatformController struct {
	version       uint64
	platformID    string
	rebootChannel chan bool
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var tmpDir string

var cfg config.Config

var storage = testStorage{
	operationState: []byte("{}"),
	moduleStatuses: make(map[string]error)}

var platform = testPlatformController{rebootChannel: make(chan bool)}

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

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetCurrentVersion(t *testing.T) {
	updater, err := updatehandler.New(&cfg, nil, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	version := updater.GetCurrentVersion()

	if version != 0 {
		t.Errorf("Wrong version: %d", version)
	}
}

func TestUpgradeRevert(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	version := updater.GetCurrentVersion()

	version++

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	if err = updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version,
		umprotocol.UpgradeOperation, umprotocol.SuccessStatus, nil); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}

	// Revert

	version--

	if err = updater.Revert(version); err != nil {
		t.Errorf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version,
		umprotocol.RevertOperation, umprotocol.SuccessStatus, nil); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}
}

func TestUpgradeFailed(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	version := updater.GetCurrentVersion()

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	modules[1].(*testModule).status = errors.New("upgrade error")

	// Upgrade

	if err = updater.Upgrade(version+1, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.FailedStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version+1,
		umprotocol.UpgradeOperation, umprotocol.FailedStatus, modules[1].(*testModule).status); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}
}

func TestRevertFailed(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	version := updater.GetCurrentVersion()

	version++

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	if err = updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	// Revert

	modules[1].(*testModule).status = errors.New("revert error")

	if err = updater.Revert(version - 1); err != nil {
		t.Fatalf("Reverting error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.FailedStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version-1,
		umprotocol.RevertOperation, umprotocol.FailedStatus, modules[1].(*testModule).status); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}
}

func TestUpgradeBadVersion(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	version := updater.GetCurrentVersion() + 1

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	if err = updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if updater.GetCurrentVersion() != version {
		t.Error("Wrong current version")
	}

	initialVersion := updater.GetCurrentVersion()

	version--

	// When a new bundle version less than current

	if err = updater.Upgrade(version, imageInfo); err == nil {
		t.Fatal("Error expected, but a new version less than a current")
	}

	if updater.GetCurrentVersion() != initialVersion {
		t.Error("Wrong current version")
	}

	// When a new bundle version equals than current

	version = initialVersion

	if err = updater.Upgrade(version, imageInfo); err == nil {
		t.Fatal("Error expected, but a new version equals than a current")
	}

	if updater.GetCurrentVersion() != initialVersion {
		t.Error("Wrong current version")
	}
}

func TestUpgradeRevertWithReboot(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	version := updater.GetCurrentVersion()

	version++

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	modules[1].(*testModule).rebootRequired = true
	modules[2].(*testModule).rebootRequired = true

	if err = updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	modules[1].(*testModule).rebootRequired = false
	modules[2].(*testModule).rebootRequired = false

	if updater, err = updatehandler.New(&cfg, modules, &platform, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version,
		umprotocol.UpgradeOperation, umprotocol.SuccessStatus, nil); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}

	// Revert

	modules[0].(*testModule).rebootRequired = true
	modules[2].(*testModule).rebootRequired = true

	version--

	if err := updater.Revert(version); err != nil {
		t.Fatalf("Reverting error: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	modules[0].(*testModule).rebootRequired = false
	modules[2].(*testModule).rebootRequired = false

	if updater, err = updatehandler.New(&cfg, modules, &platform, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version,
		umprotocol.RevertOperation, umprotocol.SuccessStatus, nil); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}

	updater.Close()
}

func TestUpgradeFailedWithReboot(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	version := updater.GetCurrentVersion()

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	modules[1].(*testModule).status = errors.New("upgrade error")
	modules[1].(*testModule).rebootRequired = true

	if err := updater.Upgrade(version+1, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	modules[1].(*testModule).status = nil
	modules[1].(*testModule).rebootRequired = false

	if updater, err = updatehandler.New(&cfg, modules, &platform, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.FailedStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version+1,
		umprotocol.UpgradeOperation, umprotocol.FailedStatus, errors.New("upgrade error")); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}

	updater.Close()
}

func TestRevertFailedWithReboot(t *testing.T) {
	modules := []updatehandler.UpdateModule{
		&testModule{id: "id1"},
		&testModule{id: "id2"},
		&testModule{id: "id3"},
	}

	updater, err := updatehandler.New(&cfg, modules, &platform, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	version := updater.GetCurrentVersion()

	version++

	imageInfo, err := createImage(path.Join(tmpDir, "testimage.bin"))
	if err != nil {
		t.Fatalf("Can't test image: %s", err)
	}

	// Upgrade

	if err := updater.Upgrade(version, imageInfo); err != nil {
		t.Fatalf("Upgrading error: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.SuccessStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	// Revert

	modules[1].(*testModule).status = errors.New("revert error")
	modules[1].(*testModule).rebootRequired = true

	if err := updater.Revert(version - 1); err != nil {
		t.Fatalf("Reverting error: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	modules[1].(*testModule).status = nil
	modules[1].(*testModule).rebootRequired = false

	if updater, err = updatehandler.New(&cfg, modules, &platform, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	if err = waitOperationFinished(updater, umprotocol.FailedStatus); err != nil {
		t.Fatalf("Operation failed: %s", err)
	}

	if err = checkOperationResult(updater, version, version-1,
		umprotocol.RevertOperation, umprotocol.FailedStatus, errors.New("revert error")); err != nil {
		t.Errorf("Wrong operation state: %s", err)
	}

	updater.Close()

}

/*******************************************************************************
 * Private
 ******************************************************************************/

func waitForRebootRequest() (err error) {
	select {
	case <-time.After(5 * time.Second):
		return errors.New("wait operation timeout")

	case <-platform.rebootChannel:
	}

	return nil
}

func waitOperationFinished(handler *updatehandler.Handler, expectedStatus string) (err error) {
	select {
	case <-time.After(5 * time.Second):
		return errors.New("wait operation timeout")

	case status := <-handler.StatusChannel():
		if status != expectedStatus {
			return errors.New("wrong operation status")
		}
	}

	return nil
}

func checkOperationResult(handler *updatehandler.Handler, currentVersion,
	operationVersion uint64, lastOperation, status string, lastError error) (err error) {
	if handler.GetCurrentVersion() != currentVersion {
		return errors.New("wrong current version")
	}

	if handler.GetOperationVersion() != operationVersion {
		return errors.New("wrong operation version")
	}

	if handler.GetLastOperation() != lastOperation {
		return errors.New("wrong last operation")
	}

	if handler.GetStatus() != status {
		return errors.New("wrong status")
	}

	handlerErrorStr := ""

	if handler.GetLastError() != nil {
		handlerErrorStr = handler.GetLastError().Error()
	}

	lastErrorStr := ""

	if lastError != nil {
		lastErrorStr = lastError.Error()
	}

	if handlerErrorStr != lastErrorStr {
		return errors.New("wrong last error")
	}

	return nil
}

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
	return module.status
}

func (module *testModule) Upgrade(version uint64, path string) (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) FinishUpgrade(version uint64) (err error) {
	return module.status
}

func (module *testModule) Revert(version uint64) (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) CancelRevert(version uint64) (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) FinishRevert(version uint64) (err error) {
	return module.status
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

func (platform *testPlatformController) SystemReboot() (err error) {
	platform.rebootChannel <- true

	return nil
}
