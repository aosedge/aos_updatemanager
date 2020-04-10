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
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strconv"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Variables
 ******************************************************************************/

var dbPath string

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err := ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	dbPath = path.Join(tmpDir, "test.db")

	ret := m.Run()

	if err = os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestDBVersion(t *testing.T) {
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = db.setVersion(dbVersion - 1); err != nil {
		t.Errorf("Can't set database version: %s", err)
	}

	db.Close()

	db, err = New(dbPath)
	if err == nil {
		t.Error("Expect version mismatch error")
	} else if err != ErrVersionMismatch {
		t.Errorf("Can't create database: %s", err)
	}

	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatalf("Can't remove database: %s", err)
	}

	db.Close()
}

func TestNewErrors(t *testing.T) {
	// Check MkdirAll in New statement
	db, err := New("/sys/rooooot/test.db")
	if err == nil {
		db.Close()
		t.Fatal("expecting error with no access rights")
	}

	//Trying to create test.db with no access rights
	//Check fail of the createConfigTable
	db, err = New("/sys/test.db")
	if err == nil {
		db.Close()
		t.Fatal("Expecting error with no access rights")
	}
}

func TestOperationState(t *testing.T) {
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}
	defer db.Close()

	setState := []byte("{This is test}")

	if err := db.SetOperationState(setState); err != nil {
		t.Fatalf("Can't set state: %s", err)
	}

	getState, err := db.GetOperationState()
	if err != nil {
		t.Fatalf("Can't get state: %s", err)
	}

	if !reflect.DeepEqual(setState, getState) {
		t.Fatalf("Wrong state value: %v", getState)
	}
}

func TestSystemVersion(t *testing.T) {
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}
	defer db.Close()

	var setVersion uint64 = 32

	if err := db.SetSystemVersion(setVersion); err != nil {
		t.Fatalf("Can't set system version: %s", err)
	}

	getVersion, err := db.GetSystemVersion()
	if err != nil {
		t.Fatalf("Can't get system version: %s", err)
	}

	if setVersion != getVersion {
		t.Fatalf("Wrong system version value: %d", getVersion)
	}
}

func TestModuleState(t *testing.T) {
	type testItem struct {
		id    string
		state string
	}

	testData := []testItem{
		{"id0", "state00"},
		{"id1", "state01"},
		{"id2", "state02"},
		{"id3", "state03"},
		{"id0", "state10"},
		{"id1", "state11"},
		{"id2", "state12"},
		{"id3", "state13"},
	}

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}
	defer db.Close()

	for i, item := range testData {
		if err = db.SetModuleState(item.id, []byte(item.state)); err != nil {
			t.Errorf("Index: %d, can't set module state: %s", i, err)
		}

		state, err := db.GetModuleState(item.id)
		if err != nil {
			t.Errorf("Index: %d, can't get module state: %s", i, err)
		}

		if string(state) != item.state {
			t.Errorf("Index: %d, wrong module state: %s", i, string(state))
		}
	}
}

func TestMultiThread(t *testing.T) {
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}
	defer db.Close()

	const numIterations = 1000

	var wg sync.WaitGroup

	wg.Add(4)

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if err := db.SetSystemVersion(uint64(i)); err != nil {
				t.Fatalf("Can't set system version: %s", err)
			}
		}
	}()

	go func() {
		defer wg.Done()

		if _, err := db.GetSystemVersion(); err != nil {
			t.Fatalf("Can't get system version: %s", err)
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if err := db.SetOperationState([]byte(strconv.Itoa(i))); err != nil {
				t.Fatalf("Can't set state: %s", err)
			}
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if _, err := db.GetOperationState(); err != nil {
				t.Fatalf("Can't get state: %s", err)
			}
		}
	}()

	wg.Wait()
}
