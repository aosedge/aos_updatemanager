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

package sshmodule_test

import (
	"io/ioutil"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/sshmodule"
)

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

	configJSON := `{
		"Host": "localhost:22",
		"User": "test",
		"Password": "test",
		"DestPath": "/tmp/remoteTestFile",
		"Commands":[
			"cd . ",
			"pwd",
			"ls"
		]
	}`

	module, err = sshmodule.New("TestComponent", []byte(configJSON), nil)
	if err != nil {
		log.Fatalf("Can't create SSH module: %s", err)
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
	if id != "TestComponent" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestUpgrade(t *testing.T) {
	if err := ioutil.WriteFile("tmp/testfile", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if _, err := module.Upgrade(1, "tmp/testfile"); err != nil {
		t.Errorf("Upgrade failed: %s", err)
	}
}

func TestWrongJson(t *testing.T) {
	configJSON := `{
		Wrong json format
		]
	}`

	module, err := sshmodule.New("TestComponent", []byte(configJSON), nil)
	if err == nil {
		module.Close()
		log.Fatalf("Expecting error here")
	}
}

func TestUpgradeErrors(t *testing.T) {
	// NOTE: test with nonexisting host
	configJSON := `{
		"Host": "localhst:22",
		"User": "test",
		"Password": "test",
		"DestPath": "/tmp/remoteTestFile",
		"Commands":[
			"cd . ",
			"pwd",
			"ls"
		]
	}`

	module, err := sshmodule.New("TestComponent", []byte(configJSON), nil)
	if err != nil {
		log.Fatalf("Error creating module %s", err)
	}
	defer module.Close()

	if err := ioutil.WriteFile("tmp/testfile", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if _, err := module.Upgrade(1, "tmp/testfile"); err == nil {
		t.Errorf("Error expected because of wrong address")
	}
}

func TestUpgradeWrongCommands(t *testing.T) {
	// NOTE: test with some wrong command
	configJSON := `{
		"Host": "localhost:22",
		"User": "test",
		"Password": "test",
		"DestPath": "/tmp/remoteTestFile",
		"Commands":[
			"cd . ",
			"some wrong command",
			"ls"
		]
	}`

	module, err := sshmodule.New("TestComponent", []byte(configJSON), nil)
	if err != nil {
		log.Fatalf("Error creating module %s", err)
	}
	defer module.Close()

	if err := ioutil.WriteFile("tmp/testfile", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	//NOTE: amoi - leaving this test to be failed right now. runCommands should handle error.
	if _, err := module.Upgrade(1, "tmp/testfile"); err == nil {
		t.Errorf("Error expected because set of commands is wrong")
	}
}

func TestModuleRevert(t *testing.T) {
	if _, err := module.Revert(0); err != nil {
		t.Errorf("Revert failed %s", err)
	}
}
