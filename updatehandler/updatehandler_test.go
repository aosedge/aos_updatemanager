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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"reflect"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/image"
	"gitpct.epam.com/epmd-aepr/aos_common/umprotocol"

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
	updateState []byte
}

type testModule struct {
	id             string
	status         error
	rebootRequired bool
	vendorVersion  string
}

type testPlatformController struct {
	rebootChannel chan bool
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var tmpDir string

var cfg config.Config

var platform = testPlatformController{rebootChannel: make(chan bool)}

var components = []*testModule{}

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

	cfg = config.Config{
		UpdateModules: []config.ModuleConfig{
			{ID: "id1", Plugin: "testmodule"},
			{ID: "id2", Plugin: "testmodule"},
			{ID: "id3", Plugin: "testmodule"}}}

	updatehandler.RegisterPlugin("testmodule",
		func(id string, configJSON json.RawMessage, controller updatehandler.PlatformController,
			storage updatehandler.StateStorage) (module updatehandler.UpdateModule, err error) {
			testModule := &testModule{id: id}

			components = append(components, testModule)

			return testModule, nil
		})

	updatehandler.RegisterControllerPlugin(func(storage updatehandler.StateStorage,
		modules []config.ModuleConfig) (controller updatehandler.PlatformController, err error) {
		return &platform, nil
	})

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestUpdate(t *testing.T) {
	components = make([]*testModule, 0, 3)

	updater, err := updatehandler.New(&cfg, &testStorage{})
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	var newStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		newStatus = append(newStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalling,
		})
	}

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	// Wait for final status

	var finalStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		finalStatus = append(finalStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalled,
		})
	}

	if err = waitForStatus(updater, finalStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}
}

func TestUpdateFailed(t *testing.T) {
	components = make([]*testModule, 0, 3)

	updater, err := updatehandler.New(&cfg, &testStorage{})
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	var newStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		newStatus = append(newStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalling,
		})
	}

	failedComponent := components[1]

	failedComponent.status = errors.New("update error")

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	// Wait for final status

	for _, info := range infos {
		if failedComponent.id == info.ID {
			currentStatus = append(currentStatus, umprotocol.ComponentStatus{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umprotocol.StatusError,
				Error:         failedComponent.status.Error(),
			})
		}
	}

	if err = waitForStatus(updater, currentStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}
}

func TestUpdateWrongVersion(t *testing.T) {
	components = make([]*testModule, 0, 3)

	updater, err := updatehandler.New(&cfg, &testStorage{})
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	// Test same vendor version

	sameVersionComponent := components[0]

	sameVersionComponent.vendorVersion = "1.0"

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	var newStatus []umprotocol.ComponentStatus

	for i, info := range infos {
		if info.ID == sameVersionComponent.id {
			infos[i].VendorVersion = sameVersionComponent.vendorVersion
		} else {
			newStatus = append(newStatus, umprotocol.ComponentStatus{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umprotocol.StatusInstalling,
			})
		}
	}

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	// Wait for final status

	var finalStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		status := umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalled,
		}

		if sameVersionComponent.id == info.ID {
			status.AosVersion = info.AosVersion - 1
		}

		finalStatus = append(finalStatus, status)
	}

	if err = waitForStatus(updater, finalStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	sameVersionComponent.vendorVersion = ""

	// Test same Aos version

	sameVersionComponent = components[1]

	currentStatus = updater.GetStatus()

	if infos, err = createUpdateInfos(currentStatus); err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus = nil

	for i, info := range infos {
		if info.ID == sameVersionComponent.id {
			infos[i].AosVersion = infos[i].AosVersion - 1
		} else {
			newStatus = append(newStatus, umprotocol.ComponentStatus{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umprotocol.StatusInstalling,
			})
		}
	}

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	// Wait for final status

	finalStatus = nil

	for _, info := range infos {
		status := umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalled,
		}

		finalStatus = append(finalStatus, status)
	}

	if err = waitForStatus(updater, finalStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}
}

func TestUpdateWithReboot(t *testing.T) {
	components = make([]*testModule, 0, 3)
	storage := testStorage{}

	updater, err := updatehandler.New(&cfg, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	components[1].rebootRequired = true
	components[2].rebootRequired = true

	var newStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		newStatus = append(newStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalling,
		})
	}

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	components = make([]*testModule, 0, 3)

	if updater, err = updatehandler.New(&cfg, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	// Wait for final status

	var finalStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		finalStatus = append(finalStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalled,
		})
	}

	if err = waitForStatus(updater, finalStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	updater.Close()
}

func TestUpdateFailedWithReboot(t *testing.T) {
	components = make([]*testModule, 0, 3)
	storage := testStorage{}

	updater, err := updatehandler.New(&cfg, &storage)
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedComponent := components[1]

	failedComponent.status = errors.New("upgrade error")
	failedComponent.rebootRequired = true

	var newStatus []umprotocol.ComponentStatus

	for _, info := range infos {
		newStatus = append(newStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusInstalling,
		})
	}

	// Update

	updater.Update(infos)

	// Wait for initial status

	if err = waitForStatus(updater, append(currentStatus, newStatus...)); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	if err = waitForRebootRequest(); err != nil {
		t.Fatalf("Reboot request failed: %s", err)
	}

	// Reboot

	updater.Close()

	components = make([]*testModule, 0, 3)

	if updater, err = updatehandler.New(&cfg, &storage); err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}

	// Wait for final status

	for _, info := range infos {
		if failedComponent.id == info.ID {
			currentStatus = append(currentStatus, umprotocol.ComponentStatus{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umprotocol.StatusError,
				Error:         failedComponent.status.Error(),
			})
		}
	}

	if err = waitForStatus(updater, currentStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	updater.Close()
}

func TestUpdateBadImage(t *testing.T) {
	components = make([]*testModule, 0, 3)

	updater, err := updatehandler.New(&cfg, &testStorage{})
	if err != nil {
		t.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	currentStatus := updater.GetStatus()

	infos, err := createUpdateInfos(currentStatus)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	for i := range infos {
		infos[i].Sha512 = []byte{1, 2, 3, 4, 5, 6}
	}

	// Update

	updater.Update(infos)

	// Wait for final status

	for _, info := range infos {
		currentStatus = append(currentStatus, umprotocol.ComponentStatus{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umprotocol.StatusError,
			Error:         "checksum sha512 mistmatch",
		})
	}

	if err = waitForStatus(updater, currentStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createUpdateInfos(currentStatus []umprotocol.ComponentStatus) (infos []umprotocol.ComponentInfo, err error) {
	for _, status := range currentStatus {
		imagePath := path.Join(tmpDir, fmt.Sprintf("testimage_%s.bin", status.ID))

		imageInfo, err := createImage(imagePath)
		if err != nil {
			return nil, err
		}

		info := umprotocol.ComponentInfo{
			ID:         status.ID,
			AosVersion: status.AosVersion + 1,
			Path:       imagePath,
			Sha256:     imageInfo.Sha256,
			Sha512:     imageInfo.Sha512,
			Size:       imageInfo.Size,
		}

		infos = append(infos, info)
	}

	return infos, nil
}

func waitForRebootRequest() (err error) {
	select {
	case <-time.After(5 * time.Second):
		return errors.New("wait operation timeout")

	case <-platform.rebootChannel:
	}

	return nil
}

func waitForStatus(handler *updatehandler.Handler, expectedStatus []umprotocol.ComponentStatus) (err error) {
	select {
	case <-time.After(5 * time.Second):
		return errors.New("wait operation timeout")

	case currentStatus := <-handler.StatusChannel():
		if expectedStatus == nil {
			return nil
		}

		for _, expectedItem := range expectedStatus {
			index := len(expectedStatus)

			for i, currentItem := range currentStatus {
				if reflect.DeepEqual(currentItem, expectedItem) {
					index = i

					break
				}
			}

			if index == len(expectedStatus) {
				return fmt.Errorf("expected item not found: %v", expectedItem)
			}

			currentStatus = append(currentStatus[:index], currentStatus[index+1:]...)
		}

		if len(currentStatus) != 0 {
			return fmt.Errorf("unexpected item found: %v", currentStatus[0])
		}

		return nil
	}
}

func createImage(imagePath string) (fileInfo image.FileInfo, err error) {
	if err := exec.Command("dd", "if=/dev/null", "of="+imagePath, "bs=1M", "count=8").Run(); err != nil {
		return fileInfo, err
	}

	return image.CreateFileInfo(imagePath)
}

func (storage *testStorage) SetUpdateState(state []byte) (err error) {
	storage.updateState = state

	return nil
}

func (storage *testStorage) GetUpdateState() (state []byte, err error) {
	return storage.updateState, nil
}

func (storage *testStorage) GetModuleState(id string) (state []byte, err error) {
	return []byte("valid"), nil
}

func (storage *testStorage) SetModuleState(id string, state []byte) (err error) {
	return nil
}

func (storage *testStorage) GetControllerState(controllerID string, name string) (value []byte, err error) {
	return []byte("valid"), nil
}

func (storage *testStorage) SetControllerState(controllerID string, name string, value []byte) (err error) {
	return nil
}

func (module *testModule) GetID() (id string) {
	return module.id
}

func (module *testModule) GetVendorVersion() (version string, err error) {
	return module.vendorVersion, nil
}

func (module *testModule) Init() (err error) {
	return module.status
}

func (module *testModule) Update(imagePath string, vendorVersion string, annotations json.RawMessage) (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) Cancel() (rebootRequired bool, err error) {
	return module.rebootRequired, module.status
}

func (module *testModule) Finish() (err error) {
	return module.status
}

func (module *testModule) Close() (err error) {
	return nil
}

func (platform *testPlatformController) SystemReboot() (err error) {
	platform.rebootChannel <- true

	return nil
}

func (platform *testPlatformController) Close() (err error) {
	return nil
}
