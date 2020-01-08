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
	"io/ioutil"
	"os"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/image"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"

	"aos_updatemanager/config"
	"aos_updatemanager/modulemanager"
	"aos_updatemanager/modulemanager/testmodule"
	"aos_updatemanager/umserver"
	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	state     int
	filesInfo []umprotocol.UpgradeFileInfo
	version   uint64
	lastError error
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

	if err := setImageVersion("tmp/version", 43); err != nil {
		log.Fatalf("Can't set image file: %s", err)
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

	updater, err = updatehandler.New(&config.Config{
		UpgradeDir:  "tmp",
		VersionFile: "tmp/version"}, moduleManager, &testStorage{})
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

func TestGetVersion(t *testing.T) {
	version := updater.GetVersion()

	if version != 43 {
		t.Errorf("Wrong version: %d", version)
	}
}

func TestUpgradeRevert(t *testing.T) {
	version := updater.GetVersion()

	filesInfo, err := generateFilesInfo()
	if err != nil {
		t.Fatalf("Can't generate files info: %s", err)
	}

	version++

	if err := updater.Upgrade(version, filesInfo); err != nil {
		t.Errorf("Upgrading error: %s", err)
	}

	if updater.GetVersion() != version {
		t.Error("Wrong version")
	}

	if updater.GetOperationVersion() != version {
		t.Error("Wrong version")
	}

	if updater.GetState() != umserver.UpgradedState {
		t.Error("Wrong state")
	}

	if err := updater.GetLastError(); err != nil {
		t.Errorf("Upgrade error: %s", err)
	}

	version--

	if err := updater.Revert(version); err != nil {
		t.Errorf("Upgrading error: %s", err)
	}

	if updater.GetVersion() != version {
		t.Error("Wrong version")
	}

	if updater.GetOperationVersion() != version {
		t.Error("Wrong version")
	}

	if updater.GetState() != umserver.RevertedState {
		t.Error("Wrong state")
	}

	if err := updater.GetLastError(); err != nil {
		t.Errorf("Upgrade error: %s", err)
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func generateFilesInfo() (filesInfo []umprotocol.UpgradeFileInfo, err error) {
	if err = ioutil.WriteFile("tmp/image", []byte("This is image file"), 0644); err != nil {
		return nil, err
	}

	info, err := image.CreateFileInfo("tmp/image")
	if err != nil {
		return nil, err
	}

	for i := 0; i < 3; i++ {
		fileInfo := umprotocol.UpgradeFileInfo{
			Target: "id" + strconv.Itoa(i+1),
			URL:    "image",
			Sha256: info.Sha256,
			Sha512: info.Sha512,
			Size:   info.Size}
		filesInfo = append(filesInfo, fileInfo)
	}

	return filesInfo, nil
}

func setImageVersion(fileName string, version uint64) (err error) {
	if err = ioutil.WriteFile(fileName, []byte(strconv.FormatUint(version, 10)), 0644); err != nil {
		return err
	}

	return nil
}

func (storage *testStorage) SetState(state int) (err error) {
	storage.state = state
	return nil
}

func (storage *testStorage) GetState() (state int, err error) {
	return storage.state, nil
}

func (storage *testStorage) SetFilesInfo(filesInfo []umprotocol.UpgradeFileInfo) (err error) {
	storage.filesInfo = filesInfo
	return nil
}

func (storage *testStorage) GetFilesInfo() (filesInfo []umprotocol.UpgradeFileInfo, err error) {
	return storage.filesInfo, nil
}

func (storage *testStorage) SetOperationVersion(version uint64) (err error) {
	storage.version = version
	return nil
}

func (storage *testStorage) GetOperationVersion() (version uint64, err error) {
	return storage.version, nil
}

func (storage *testStorage) SetLastError(lastError error) (err error) {
	storage.lastError = lastError
	return nil
}

func (storage *testStorage) GetLastError() (lastError error, err error) {
	return storage.lastError, nil
}
