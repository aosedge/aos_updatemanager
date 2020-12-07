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
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/crthandler"
)

/*******************************************************************************
 * Variables
 ******************************************************************************/

var tmpDir string
var db *Database

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

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	dbPath := path.Join(tmpDir, "test.db")
	db, err = New(dbPath, tmpDir, tmpDir)
	if err != nil {
		log.Fatalf("Can't create database: %s", err)
	}

	ret := m.Run()

	db.Close()

	if err = os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/
func TestNewErrors(t *testing.T) {
	// Check MkdirAll in New statement
	dbLocal, err := New("/sys/rooooot/test.db", tmpDir, tmpDir)
	if err == nil {
		dbLocal.Close()
		t.Fatal("expecting error with no access rights")
	}

	//Trying to create test.db with no access rights
	//Check fail of the createConfigTable
	dbLocal, err = New("/sys/test.db", tmpDir, tmpDir)
	if err == nil {
		dbLocal.Close()
		t.Fatal("Expecting error with no access rights")
	}
}

func TestUpdateState(t *testing.T) {
	setState := []byte("{This is test}")

	if err := db.SetUpdateState(setState); err != nil {
		t.Fatalf("Can't set state: %s", err)
	}
	getState, err := db.GetUpdateState()
	if err != nil {
		t.Fatalf("Can't get state: %s", err)
	}

	if !reflect.DeepEqual(setState, getState) {
		t.Fatalf("Wrong state value: %v", getState)
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

	for i, item := range testData {
		if err := db.SetModuleState(item.id, []byte(item.state)); err != nil {
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

func TestControllerState(t *testing.T) {
	type testItem struct {
		id    string
		name  string
		value string
	}

	testData := []testItem{
		{"id0", "name00", "state00"},
		{"id0", "name01", "state00"},
		{"id1", "name01", "state01"},
		{"id1", "name02", "state01"},
		{"id2", "name02", "state02"},
		{"id2", "name03", "state02"},
		{"id3", "name03", "state03"},
		{"id0", "name10", "state10"},
		{"id0", "name10", "state11"},
		{"id1", "name11", "state11"},
		{"id2", "name12", "state12"},
		{"id3", "name13", "state13"},
	}

	for i, item := range testData {
		if err := db.SetControllerState(item.id, item.name, []byte(item.value)); err != nil {
			t.Errorf("Index: %d, can't set module state: %s", i, err)
		}

		value, err := db.GetControllerState(item.id, item.name)
		if err != nil {
			t.Errorf("Index: %d, can't get module state: %s", i, err)
		}

		if string(value) != item.value {
			t.Errorf("Index: %d, wrong module state: %s", i, string(value))
		}
	}
}

func TestMultiThread(t *testing.T) {
	const numIterations = 1000

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if err := db.SetUpdateState([]byte(strconv.Itoa(i))); err != nil {
				t.Fatalf("Can't set state: %s", err)
			}
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if _, err := db.GetUpdateState(); err != nil {
				t.Fatalf("Can't get state: %s", err)
			}
		}
	}()

	wg.Wait()
}

func TestAddRemoveCertificate(t *testing.T) {
	type testData struct {
		crtType       string
		crt           crthandler.CrtInfo
		errorExpected bool
	}

	data := []testData{
		testData{crtType: "online", crt: crthandler.CrtInfo{"issuer0", "s0", "crtURL0", "keyURL0", time.Now().UTC()}, errorExpected: false},
		testData{crtType: "online", crt: crthandler.CrtInfo{"issuer0", "s0", "crtURL0", "keyURL0", time.Now().UTC()}, errorExpected: true},
		testData{crtType: "online", crt: crthandler.CrtInfo{"issuer1", "s0", "crtURL1", "keyURL1", time.Now().UTC()}, errorExpected: false},
		testData{crtType: "online", crt: crthandler.CrtInfo{"issuer1", "s0", "crtURL1", "keyURL1", time.Now().UTC()}, errorExpected: true},
		testData{crtType: "online", crt: crthandler.CrtInfo{"issuer2", "s0", "crtURL2", "keyURL2", time.Now().UTC()}, errorExpected: false}}

	// Add certificates
	for _, item := range data {
		if err := db.AddCertificate(item.crtType, item.crt); err != nil && !item.errorExpected {
			t.Errorf("Can't add certificate: %s", err)
		}
	}

	// Get certificates

	for _, item := range data {
		crt, err := db.GetCertificate(item.crt.Issuer, item.crt.Serial)
		if err != nil && !item.errorExpected {
			t.Errorf("Can't get certificate: %s", err)

			continue
		}

		if item.errorExpected {
			continue
		}

		if !reflect.DeepEqual(crt, item.crt) {
			t.Errorf("Wrong crt info: %v", crt)
		}
	}

	// Remove certificates

	for _, item := range data {
		if err := db.RemoveCertificate(item.crtType, item.crt.CrtURL); err != nil && !item.errorExpected {
			t.Errorf("Can't remove certificate: %s", err)
		}

		if _, err := db.GetCertificate(item.crtType, item.crt.CrtURL); err == nil {
			t.Error("Certificate should be removed")
		}
	}
}

func TestGetCertificates(t *testing.T) {
	data := [][]crthandler.CrtInfo{
		[]crthandler.CrtInfo{
			crthandler.CrtInfo{"issuer0", "s0", "crtURL0", "keyURL0", time.Now().UTC()},
			crthandler.CrtInfo{"issuer0", "s1", "crtURL1", "keyURL1", time.Now().UTC()},
			crthandler.CrtInfo{"issuer0", "s2", "crtURL2", "keyURL2", time.Now().UTC()},
			crthandler.CrtInfo{"issuer0", "s3", "crtURL3", "keyURL3", time.Now().UTC()},
			crthandler.CrtInfo{"issuer0", "s4", "crtURL4", "keyURL4", time.Now().UTC()},
		},
		[]crthandler.CrtInfo{
			crthandler.CrtInfo{"issuer1", "s0", "crtURL0", "keyURL0", time.Now().UTC()},
			crthandler.CrtInfo{"issuer1", "s1", "crtURL1", "keyURL1", time.Now().UTC()},
			crthandler.CrtInfo{"issuer1", "s2", "crtURL2", "keyURL2", time.Now().UTC()},
		},
		[]crthandler.CrtInfo{
			crthandler.CrtInfo{"issuer2", "s0", "crtURL0", "keyURL0", time.Now().UTC()},
			crthandler.CrtInfo{"issuer2", "s1", "crtURL1", "keyURL1", time.Now().UTC()},
			crthandler.CrtInfo{"issuer2", "s2", "crtURL2", "keyURL2", time.Now().UTC()},
			crthandler.CrtInfo{"issuer2", "s3", "crtURL3", "keyURL3", time.Now().UTC()},
		},
	}

	for i, items := range data {
		for _, crt := range items {
			if err := db.AddCertificate("crt"+strconv.Itoa(i), crt); err != nil {
				t.Errorf("Can't add certificate: %s", err)
			}
		}
	}

	for i, items := range data {
		crts, err := db.GetCertificates("crt" + strconv.Itoa(i))
		if err != nil {
			t.Errorf("Can't get certificates: %s", err)

			continue
		}

		if !reflect.DeepEqual(crts, items) {
			t.Error("Wrong crts data")

			continue
		}
	}
}

func TestMigrationToV1(t *testing.T) {
	migrationDb := path.Join(tmpDir, "test_migration.db")

	if err := os.MkdirAll("mergedMigration", 0755); err != nil {
		t.Fatalf("Error creating service images: %s", err)
	}
	defer func() {
		if err := os.RemoveAll("mergedMigration"); err != nil {
			log.Fatalf("Error deleting tmp dir: %s", err)
		}
	}()

	if err := createDatabaseV0(migrationDb); err != nil {
		t.Fatalf("Can't create initial database %s", err)
	}

	// Migration upward
	db, err := newDatabase(migrationDb, "migration", "mergedMigration", 1)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer1(db.sql); err != nil {
		t.Fatalf("Error checking db version: %s", err)
	}

	db.Close()

	// Migration downward
	db, err = newDatabase(migrationDb, "migration", "mergedMigration", 0)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer0(db.sql); err != nil {
		t.Fatalf("Error checking db version: %s", err)
	}

	db.Close()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createDatabaseV0(name string) (err error) {
	sqlite, err := sql.Open("sqlite3", fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=%s&_sync=%s",
		name, busyTimeout, journalMode, syncMode))
	if err != nil {
		return err
	}
	defer sqlite.Close()

	if _, err = sqlite.Exec(
		`CREATE TABLE config (
			updateState TEXT, version INTEGER)`); err != nil {
		return err
	}

	if _, err = sqlite.Exec(
		`INSERT INTO config (
			updateState, version) values(?, ?)`, "", 0); err != nil {
		return err
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS modules (id TEXT NOT NULL PRIMARY KEY, state TEXT)`); err != nil {
		return err
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS modules_data (id TEXT NOT NULL PRIMARY KEY, name TEXT NOT NULL, value TEXT)`); err != nil {
		return err
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS certificates (
		type TEXT NOT NULL,
		issuer TEXT NOT NULL,
		serial TEXT NOT NULL,
		crtURL TEXT,
		keyURL TEXT,
		notAfter TIMESTAMP,
		PRIMARY KEY (issuer, serial))`); err != nil {
		return err
	}

	return nil
}

func isDatabaseVer0(sqlite *sql.DB) (err error) {
	rows, err := sqlite.Query("SELECT COUNT(*) AS CNTREC FROM pragma_table_info('config') WHERE name='version'")
	if err != nil {
		return err
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		if err = rows.Scan(&count); err != nil {
			return err
		}

		if count == 0 {
			return ErrNotExist
		}

		verRows, err := sqlite.Query("SELECT version FROM config")
		if err != nil {
			return err
		}
		defer verRows.Close()

		var version int
		for verRows.Next() {
			if err = verRows.Scan(&version); err != nil {
				return err
			}

			if version != 5 {
				return fmt.Errorf("wrong version in database: expected 5, got %d", version)
			}

			return nil
		}

		break
	}

	return ErrNotExist
}

func isDatabaseVer1(sqlite *sql.DB) (err error) {
	rows, err := sqlite.Query("SELECT COUNT(*) AS CNTREC FROM pragma_table_info('config') WHERE name='version'")
	if err != nil {
		return err
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		if err = rows.Scan(&count); err != nil {
			return err
		}

		if count != 0 {
			return ErrNotExist
		}

		return nil
	}

	return ErrNotExist
}
