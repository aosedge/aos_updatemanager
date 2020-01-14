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

package database

import (
	"errors"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Variables
 ******************************************************************************/

var db *Database

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	if err = os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}

	db, err = New("tmp/test.db")
	if err != nil {
		log.Fatalf("Can't create database: %s", err)
	}

	ret := m.Run()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	db.Close()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestDBVersion(t *testing.T) {
	db, err := New("tmp/version.db")
	if err != nil {
		log.Fatalf("Can't create database: %s", err)
	}

	if err = db.setVersion(dbVersion - 1); err != nil {
		log.Errorf("Can't set database version: %s", err)
	}

	db.Close()

	db, err = New("tmp/version.db")
	if err == nil {
		log.Error("Expect version mismatch error")
	} else if err != ErrVersionMismatch {
		log.Errorf("Can't create database: %s", err)
	}

	db.Close()
}

func TestState(t *testing.T) {
	setState := updatehandler.RevertingState
	if err := db.SetState(setState); err != nil {
		t.Fatalf("Can't set state: %s", err)
	}

	getState, err := db.GetState()
	if err != nil {
		t.Fatalf("Can't get state: %s", err)
	}

	if setState != getState {
		t.Fatalf("Wrong state value: %v", getState)
	}
}

func TestCurrentVersion(t *testing.T) {
	setVersion := uint64(16)
	if err := db.SetCurrentVersion(setVersion); err != nil {
		t.Fatalf("Can't set current version: %s", err)
	}

	getVersion, err := db.GetCurrentVersion()
	if err != nil {
		t.Fatalf("Can't get current version: %s", err)
	}

	if setVersion != getVersion {
		t.Fatalf("Wrong current version value: %v", getVersion)
	}
}

func TestOperationVersion(t *testing.T) {
	setVersion := uint64(5)
	if err := db.SetOperationVersion(setVersion); err != nil {
		t.Fatalf("Can't set operation version: %s", err)
	}

	getVersion, err := db.GetOperationVersion()
	if err != nil {
		t.Fatalf("Can't get operation version: %s", err)
	}

	if setVersion != getVersion {
		t.Fatalf("Wrong operation version value: %v", getVersion)
	}
}

func TestLastError(t *testing.T) {
	setError := errors.New("last error")
	if err := db.SetLastError(setError); err != nil {
		t.Fatalf("Can't set last error: %s", err)
	}

	getError, err := db.GetLastError()
	if err != nil {
		t.Fatalf("Can't get last error: %s", err)
	}

	if setError.Error() != getError.Error() {
		t.Fatalf("Wrong last error value: %v", getError)
	}

	setError = nil
	if err := db.SetLastError(setError); err != nil {
		t.Fatalf("Can't set last error: %s", err)
	}

	getError, err = db.GetLastError()
	if err != nil {
		t.Fatalf("Can't get last error: %s", err)
	}

	if setError != getError {
		t.Fatalf("Wrong last error value: %v", getError)
	}
}
