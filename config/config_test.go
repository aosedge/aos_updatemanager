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

/*******************************************************************************
 * Private
 ******************************************************************************/

func createConfigFile() (err error) {
	configContent := `{
	"ServerUrl": "localhost:8090",
	"Cert": "crt.pem",
	"Key": "key.pem",
	"UpgradeDir": "/var/aos/upgrade",
	"WorkingDir": "/var/aos/updatemanager",
	"Modules":[{
		"ID": "id1",
		"Disabled": true,
		"Module": "test1"
	}, {
		"ID": "id2",
		"Module": "test2"
	}, {
		"ID": "id3",
		"Module": "test3"
	}]
}`

	if err := ioutil.WriteFile(path.Join("tmp", "aos_updatemanager.cfg"), []byte(configContent), 0644); err != nil {
		return err
	}

	return nil
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
	if len(cfg.Modules) != 3 {
		t.Fatalf("Wrong modules len: %d", len(cfg.Modules))
	}

	if cfg.Modules[0].ID != "id1" || cfg.Modules[1].ID != "id2" || cfg.Modules[2].ID != "id3" {
		t.Error("Wrong module id")
	}

	if cfg.Modules[0].Module != "test1" || cfg.Modules[1].Module != "test2" || cfg.Modules[2].Module != "test3" {
		t.Error("Wrong module plugin")
	}

	if cfg.Modules[0].Disabled != true || cfg.Modules[1].Disabled != false || cfg.Modules[2].Disabled != false {
		t.Error("Wrong disable value")
	}
}

func TestGetUpgradeDir(t *testing.T) {
	if cfg.UpgradeDir != "/var/aos/upgrade" {
		t.Errorf("Wrong upgrade dir value: %s", cfg.UpgradeDir)
	}
}

func TestGetWorkingDir(t *testing.T) {
	if cfg.WorkingDir != "/var/aos/updatemanager" {
		t.Errorf("Wrong working dir value: %s", cfg.WorkingDir)
	}
}
