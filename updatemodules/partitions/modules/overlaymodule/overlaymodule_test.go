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

package overlaymodule_test

import (
	"aos_updatemanager/updatemodules/partitions/modules/overlaymodule"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/aoscloud/aos_common/aoserrors"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	state []byte
}

type testRebooter struct {
	rebootChannel chan struct{}
}

type testChecker struct {
	err error
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var (
	tmpDir      string
	versionFile string
	updateDir   string
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

	versionFile = path.Join(tmpDir, "version.txt")
	updateDir = path.Join(tmpDir, "update")

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	module, err := overlaymodule.New("test", versionFile, updateDir, &testStorage{}, nil, nil)
	if err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}
	defer module.Close()

	if module.GetID() != "test" {
		t.Errorf("Wrong module ID: %s", module.GetID())
	}
}

func TestUpdate(t *testing.T) {
	rebooter := newTestRebooter()
	storage := &testStorage{}

	if err := createVersionFile("v1.0"); err != nil {
		t.Fatalf("Can't create version file: %s", err)
	}

	// Create and init module

	module, err := overlaymodule.New("test", versionFile, updateDir, storage, rebooter, nil)
	if err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Prepare

	imagePath := path.Join(tmpDir, "rootfs.squashfs")

	if err = createImage(imagePath); err != nil {
		t.Fatalf("Can't create image: %s", err)
	}

	if err = module.Prepare(imagePath, "v2.0", json.RawMessage(`{"type":"full"}`)); err != nil {
		t.Fatalf("Prepare error: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Fatalf("Update error: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	updateContent, err := ioutil.ReadFile(path.Join(updateDir, "do_update"))
	if err != nil {
		t.Errorf("Can't read update file: %s", err)
	}

	if string(updateContent) != "full" {
		t.Errorf("Wrong update file content: %s", updateContent)
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Fatalf("Reboot error: %s", err)
	}

	if err = rebooter.waitForReboot(); err != nil {
		t.Fatalf("Wait for reboot error: %s", err)
	}

	// Restart and init module

	module.Close()

	if err = ioutil.WriteFile(path.Join(updateDir, "updated"), nil, 0o600); err != nil {
		t.Fatalf("Can't create updated file: %s", err)
	}

	if err := createVersionFile("v2.0"); err != nil {
		t.Fatalf("Can't create version file: %s", err)
	}

	if module, err = overlaymodule.New("test", versionFile, updateDir, storage, rebooter, nil); err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err != nil {
		t.Fatalf("Update error: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Apply

	if rebootRequired, err = module.Apply(); err != nil {
		t.Fatalf("Apply error: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	applyContent, err := ioutil.ReadFile(path.Join(updateDir, "do_apply"))
	if err != nil {
		t.Errorf("Can't read apply file: %s", err)
	}

	if string(applyContent) != "full" {
		t.Errorf("Wrong apply file content: %s", applyContent)
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Fatalf("Reboot error: %s", err)
	}

	if err = rebooter.waitForReboot(); err != nil {
		t.Fatalf("Wait for reboot error: %s", err)
	}

	// Restart and init module

	module.Close()

	if module, err = overlaymodule.New("test", versionFile, updateDir, storage, rebooter, nil); err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Apply

	if rebootRequired, err = module.Apply(); err != nil {
		t.Fatalf("Apply error: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Check

	vendorVersion, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	if vendorVersion != "v2.0" {
		t.Errorf("Wrong vendor version: %s", err)
	}

	if _, err = os.Stat(path.Join(updateDir, "do_update")); err == nil {
		t.Error("Update file should be deleted")
	}

	if _, err = os.Stat(path.Join(updateDir, "do_apply")); err == nil {
		t.Error("Apply file should be deleted")
	}

	if _, err = os.Stat(path.Join(updateDir, "updated")); err == nil {
		t.Error("Updated file should be deleted")
	}

	module.Close()
}

func TestUpdateFail(t *testing.T) {
	rebooter := newTestRebooter()
	storage := &testStorage{}

	if err := createVersionFile("v1.0"); err != nil {
		t.Fatalf("Can't create version file: %s", err)
	}

	// Create and init module

	module, err := overlaymodule.New("test", versionFile, updateDir, storage, rebooter, nil)
	if err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Prepare

	imagePath := path.Join(tmpDir, "rootfs")

	if err = createImage(imagePath); err != nil {
		t.Fatalf("Can't create image: %s", err)
	}

	if err = module.Prepare(imagePath, "v3.0", json.RawMessage(`{"type":"full"}`)); err != nil {
		t.Fatalf("Prepare error: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Fatalf("Update error: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	updateContent, err := ioutil.ReadFile(path.Join(updateDir, "do_update"))
	if err != nil {
		t.Errorf("Can't read update file: %s", err)
	}

	if string(updateContent) != "full" {
		t.Errorf("Wrong update file content: %s", updateContent)
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Fatalf("Reboot error: %s", err)
	}

	if err = rebooter.waitForReboot(); err != nil {
		t.Fatalf("Wait for reboot error: %s", err)
	}

	// Restart and init module

	module.Close()

	if module, err = overlaymodule.New("test", versionFile, updateDir, storage, rebooter, nil); err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err == nil {
		t.Fatal("Update should fail")
	}

	if rebootRequired {
		t.Error("Reboot is not required")
	}

	// Revert

	if rebootRequired, err = module.Revert(); err != nil {
		t.Fatalf("Revert error: %s", err)
	}

	if rebootRequired {
		t.Error("Reboot is not required")
	}

	if _, err = os.Stat(path.Join(updateDir, "do_update")); err == nil {
		t.Error("Update file should be deleted")
	}

	if _, err = os.Stat(path.Join(updateDir, "do_apply")); err == nil {
		t.Error("Apply file should be deleted")
	}

	if _, err = os.Stat(path.Join(updateDir, "updated")); err == nil {
		t.Error("Updated file should be deleted")
	}

	module.Close()
}

func TestUpdateChecker(t *testing.T) {
	rebooter := newTestRebooter()
	storage := &testStorage{}

	if err := createVersionFile("v1.0"); err != nil {
		t.Fatalf("Can't create version file: %s", err)
	}

	// Create and init module

	module, err := overlaymodule.New("test", versionFile, updateDir, storage, rebooter, newTestChecker(nil))
	if err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Prepare

	imagePath := path.Join(tmpDir, "rootfs")

	if err = createImage(imagePath); err != nil {
		t.Fatalf("Can't create image: %s", err)
	}

	if err = module.Prepare(imagePath, "v3.0", json.RawMessage(`{"type":"full"}`)); err != nil {
		t.Fatalf("Prepare error: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Fatalf("Update error: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	updateContent, err := ioutil.ReadFile(path.Join(updateDir, "do_update"))
	if err != nil {
		t.Errorf("Can't read update file: %s", err)
	}

	if string(updateContent) != "full" {
		t.Errorf("Wrong update file content: %s", updateContent)
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Fatalf("Reboot error: %s", err)
	}

	if err = rebooter.waitForReboot(); err != nil {
		t.Fatalf("Wait for reboot error: %s", err)
	}

	// Restart and init module

	module.Close()

	if err := createVersionFile("v3.0"); err != nil {
		t.Fatalf("Can't create version file: %s", err)
	}

	if err = ioutil.WriteFile(path.Join(updateDir, "updated"), nil, 0o600); err != nil {
		t.Fatalf("Can't create updated file: %s", err)
	}

	if module, err = overlaymodule.New("test", versionFile, updateDir, storage, rebooter,
		newTestChecker(aoserrors.New("update failed"))); err != nil {
		t.Fatalf("Can't create overlay module: %s", err)
	}

	if err = module.Init(); err != nil {
		t.Fatalf("Can't initialize module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err == nil {
		t.Fatal("Update should fail")
	}

	if rebootRequired {
		t.Error("Reboot is not required")
	}

	module.Close()
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

func newTestRebooter() (rebooter *testRebooter) {
	return &testRebooter{rebootChannel: make(chan struct{}, 1)}
}

func (rebooter *testRebooter) Reboot() (err error) {
	rebooter.rebootChannel <- struct{}{}

	return nil
}

func (rebooter *testRebooter) waitForReboot() (err error) {
	select {
	case <-rebooter.rebootChannel:
		return nil

	case <-time.After(1 * time.Second):
		return aoserrors.New("wait reboot timeout")
	}
}

func newTestChecker(err error) (checker *testChecker) {
	return &testChecker{err: err}
}

func (checker *testChecker) Check() (err error) {
	return checker.err
}

func createImage(imagePath string) (err error) {
	if err = os.MkdirAll(path.Dir(imagePath), 0o755); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(imagePath, []byte("this is update image"), 0o600); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func createVersionFile(version string) (err error) {
	if err = os.MkdirAll(path.Dir(versionFile), 0o755); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(versionFile, []byte(fmt.Sprintf(`VERSION="%s"`, version)), 0o600); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
