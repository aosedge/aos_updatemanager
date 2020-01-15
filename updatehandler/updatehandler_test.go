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
	"os"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"

	"aos_updatemanager/config"
	"aos_updatemanager/modulemanager"
	"aos_updatemanager/modulemanager/testmodule"
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
	imageInfo        umprotocol.ImageInfo
	operationVersion uint64
	currentVersion   uint64
	lastError        error
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var updater *updatehandler.Handler

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

	if err := os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Can't crate tmp dir: %s", err)
	}

	modulemanager.Register(testmodule.Name, func(id string, configJSON []byte) (module interface{}, err error) {
		return testmodule.New(id, configJSON)
	})

	moduleManager, err := modulemanager.New(&config.Config{
		Modules: []config.ModuleConfig{
			config.ModuleConfig{
				ID:     "id1",
				Module: "test"},
			config.ModuleConfig{
				ID:     "id2",
				Module: "test"},
			config.ModuleConfig{
				ID:     "id3",
				Module: "test"}}})
	if err != nil {
		log.Fatalf("Can't create module manager: %s", err)
	}

	updater, err = updatehandler.New(&config.Config{UpgradeDir: "tmp"}, moduleManager, &testStorage{})
	if err != nil {
		log.Fatalf("Can't create updater: %s", err)
	}

	ret := m.Run()

	updater.Close()

	if err := os.RemoveAll("tmp"); err != nil {
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
	statusChannel := make(chan string)

	updater.SetStatusCallback(func(status string) {
		statusChannel <- status
	})

	version := updater.GetCurrentVersion()

	version++

	if err := updater.Upgrade(version, umprotocol.ImageInfo{}); err != nil {
		t.Errorf("Upgrading error: %s", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Error("wait operation timeout")

	case <-statusChannel:
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

	case <-statusChannel:
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

/*******************************************************************************
 * Private
 ******************************************************************************/

func (storage *testStorage) SetState(state int) (err error) {
	storage.state = state
	return nil
}

func (storage *testStorage) GetState() (state int, err error) {
	return storage.state, nil
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
