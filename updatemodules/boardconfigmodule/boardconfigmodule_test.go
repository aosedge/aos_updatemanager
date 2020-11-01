// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2020 Renesas Inc.
// Copyright 2020 EPAM Systems Inc.
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

package boardconfigmodule_test

import (
	"io/ioutil"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/boardconfigmodule"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

const boardFileName = "tmp/test_configuration.cfg"

/*******************************************************************************
 * Var
 ******************************************************************************/

var module updatehandler.UpdateModule

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
		log.Fatalf("Error creating tmp dir %s", err)
	}

	configJSON := `{"path": "tmp/test_configuration.cfg"}`

	module, err = boardconfigmodule.New("boardConfig", []byte(configJSON), nil)
	if err != nil {
		log.Fatalf("Can't create file update module module: %s", err)
	}

	ret := m.Run()

	module.Close()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id := module.GetID()
	if id != "boardConfig" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestGetVersion(t *testing.T) {
	board_file := `
	{
		"vendorVersion": "V1.0",
		"resources": [{
			"id": "testConfig",
			"resource": "testConfig.gz"
		}]
	}
	`
	if err := ioutil.WriteFile(boardFileName, []byte(board_file), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	version, err := module.GetVendorVersion()
	if err != nil {
		log.Fatalf("Can't get board vendor version: %s", err)
	}

	if version != "V1.0" {
		t.Errorf("Wrong board config version: %s", version)
	}
}
