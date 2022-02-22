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

package config_test

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"

	"github.com/aoscloud/aos_common/aoserrors"

	"github.com/aoscloud/aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const wrongConfigName = "aos_wrongconfig.cfg"

/*******************************************************************************
 * Vars
 ******************************************************************************/

var cfg *config.Config

/*******************************************************************************
 * Private
 ******************************************************************************/

func saveConfigFile(configName string, configContent string) (err error) {
	if err = ioutil.WriteFile(path.Join("tmp", configName), []byte(configContent), 0o600); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func createWrongConfigFile() (err error) {
	configContent := ` SOME WRONG JSON FORMAT
	}]
}`

	return saveConfigFile(wrongConfigName, configContent)
}

func createConfigFile() (err error) {
	configContent := `{
	"ServerUrl": "localhost:8090",
	"ID": "um01",
	"CACert": "/etc/ssl/certs/rootCA.crt",
	"CertStorage": "um",
	"WorkingDir": "/var/aos/updatemanager",
	"DownloadDir": "/var/aos/updatemanager/download",
	"UpdateModules":[{
		"ID": "id1",
		"Plugin": "test1",
		"UpdatePriority": 1,
		"RebootPriority": 1,
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}, {
		"ID": "id2",
		"Plugin": "test2",
		"UpdatePriority": 2,
		"RebootPriority": 2,
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}, {
		"ID": "id3",
		"Plugin": "test3",
		"UpdatePriority": 3,
		"RebootPriority": 3,
		"Disabled": true,
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}],
	"migration": {
		"migrationPath" : "/usr/share/aos_updatemanager/migration",
		"mergedMigrationPath" : "/var/aos/updatemanager/mergedMigrationPath"
	}
}`

	return saveConfigFile("aos_updatemanager.cfg", configContent)
}

func setup() (err error) {
	if err := os.MkdirAll("tmp", 0o755); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = createConfigFile(); err != nil {
		return aoserrors.Wrap(err)
	}

	if cfg, err = config.New("tmp/aos_updatemanager.cfg"); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func cleanup() (err error) {
	if err := os.RemoveAll("tmp"); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		log.Fatalf("Setup error: %s", err)
	}

	ret := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Cleanup error: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	if cfg.ID != "um01" {
		t.Errorf("Wrong ID value: %s", cfg.ID)
	}
}

func TestGetCredentials(t *testing.T) {
	if cfg.ServerURL != "localhost:8090" {
		t.Errorf("Wrong ServerURL value: %s", cfg.ServerURL)
	}

	if cfg.CACert != "/etc/ssl/certs/rootCA.crt" {
		t.Errorf("Wrong caCert value: %s", cfg.CACert)
	}

	if cfg.CertStorage != "um" {
		t.Errorf("Wrong certStorage value: %s", cfg.CertStorage)
	}
}

func TestModules(t *testing.T) {
	if len(cfg.UpdateModules) != 3 {
		t.Fatalf("Wrong modules len: %d", len(cfg.UpdateModules))
	}

	if cfg.UpdateModules[0].ID != "id1" || cfg.UpdateModules[1].ID != "id2" ||
		cfg.UpdateModules[2].ID != "id3" {
		t.Error("Wrong module id")
	}

	if cfg.UpdateModules[0].Plugin != "test1" || cfg.UpdateModules[1].Plugin != "test2" ||
		cfg.UpdateModules[2].Plugin != "test3" {
		t.Error("Wrong plugin value")
	}

	if cfg.UpdateModules[0].UpdatePriority != 1 || cfg.UpdateModules[1].UpdatePriority != 2 ||
		cfg.UpdateModules[2].UpdatePriority != 3 {
		t.Error("Wrong update priority value")
	}

	if cfg.UpdateModules[0].RebootPriority != 1 || cfg.UpdateModules[1].RebootPriority != 2 ||
		cfg.UpdateModules[2].RebootPriority != 3 {
		t.Error("Wrong reboot priority value")
	}

	if cfg.UpdateModules[0].Disabled != false || cfg.UpdateModules[1].Disabled != false ||
		cfg.UpdateModules[2].Disabled != true {
		t.Error("Disabled value")
	}
}

func TestGetWorkingDir(t *testing.T) {
	if cfg.WorkingDir != "/var/aos/updatemanager" {
		t.Errorf("Wrong working dir value: %s", cfg.WorkingDir)
	}
}

func TestGetDownloadDir(t *testing.T) {
	if cfg.DownloadDir != "/var/aos/updatemanager/download" {
		t.Errorf("Wrong download dir value: %s", cfg.DownloadDir)
	}
}

func TestNewErrors(t *testing.T) {
	// Executing new statement with nonexisting config file
	if _, err := config.New("some_nonexisting_file"); err == nil {
		t.Errorf("No error was returned for nonexisting config")
	}

	// Creating wrong config
	if err := createWrongConfigFile(); err != nil {
		t.Errorf("Unable to create wrong config file. Err %s", err)
	}

	// Testing with wrong json format
	if _, err := config.New(path.Join("tmp", wrongConfigName)); err == nil {
		t.Errorf("No error was returned for config with wrong format")
	}
}

func TestDatabaseMigration(t *testing.T) {
	if cfg.Migration.MigrationPath != "/usr/share/aos_updatemanager/migration" {
		t.Errorf("Wrong migrationPath /usr/share/aos_updatemanager/migration, != %s", cfg.Migration.MigrationPath)
	}

	if cfg.Migration.MergedMigrationPath != "/var/aos/updatemanager/mergedMigrationPath" {
		t.Errorf("Wrong migrationPath /var/aos/updatemanager/mergedMigrationPath, != %s", cfg.Migration.MergedMigrationPath)
	}
}
