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

package config_test

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var cfg *config.Config
var wrongConfigName = "aos_wrongconfig.cfg"

/*******************************************************************************
 * Private
 ******************************************************************************/

func saveConfigFile(configName string, configContent string) (err error) {
	if err = ioutil.WriteFile(path.Join("tmp", configName), []byte(configContent), 0644); err != nil {
		return err
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
	"Cert": "crt.pem",
	"Key": "key.pem",
	"WorkingDir": "/var/aos/updatemanager",
	"UpdateModules":[{
		"ID": "id1",
		"Plugin": "test1",
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}, {
		"ID": "id2",
		"Plugin": "test2",
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}, {
		"ID": "id3",
		"Plugin": "test3",
		"Disabled": true,
		"Params": {
			"Param1" :"value1",
			"Param2" : 2
		}
	}]
}`

	return saveConfigFile("aos_updatemanager.cfg", configContent)
}

func setup() (err error) {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return err
	}

	if err = createConfigFile(); err != nil {
		return err
	}

	if cfg, err = config.New("tmp/aos_updatemanager.cfg"); err != nil {
		return err
	}

	return nil
}

func cleanup() (err error) {
	if err := os.RemoveAll("tmp"); err != nil {
		return err
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

func TestGetCredentials(t *testing.T) {
	if cfg.ServerURL != "localhost:8090" {
		t.Errorf("Wrong ServerURL value: %s", cfg.ServerURL)
	}

	if cfg.Cert != "crt.pem" {
		t.Errorf("Wrong cert value: %s", cfg.Cert)
	}

	if cfg.Key != "key.pem" {
		t.Errorf("Wrong key value: %s", cfg.Key)
	}
}
func TestModules(t *testing.T) {
	if len(cfg.UpdateModules) != 3 {
		t.Fatalf("Wrong modules len: %d", len(cfg.UpdateModules))
	}

	if cfg.UpdateModules[0].ID != "id1" || cfg.UpdateModules[1].ID != "id2" || cfg.UpdateModules[2].ID != "id3" {
		t.Error("Wrong module id")
	}

	if cfg.UpdateModules[0].Plugin != "test1" || cfg.UpdateModules[1].Plugin != "test2" || cfg.UpdateModules[2].Plugin != "test3" {
		t.Error("Wrong plugin value")
	}

	if cfg.UpdateModules[0].Disabled != false || cfg.UpdateModules[1].Disabled != false || cfg.UpdateModules[2].Disabled != true {
		t.Error("Disabled value")
	}
}

func TestGetWorkingDir(t *testing.T) {
	if cfg.WorkingDir != "/var/aos/updatemanager" {
		t.Errorf("Wrong working dir value: %s", cfg.WorkingDir)
	}
}

func TestNewErrors(t *testing.T) {
	// Executing new statement with nonexisting config file
	if _, err := config.New("some_nonexisting_file"); err == nil {
		t.Errorf("No error was returned for nonexisting config")
	}

	//Creating wrong config
	if err := createWrongConfigFile(); err != nil {
		t.Errorf("Unable to create wrong config file. Err %s", err)
	}

	// Testing with wrong json format
	if _, err := config.New(path.Join("tmp", wrongConfigName)); err == nil {
		t.Errorf("No error was returned for config with wrong format")
	}
}
