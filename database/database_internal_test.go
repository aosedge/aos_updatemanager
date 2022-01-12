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

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/migration"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Variables
 ******************************************************************************/

const migrationDir = "migration"

/*******************************************************************************
 * Variables
 ******************************************************************************/

var (
	tmpDir string
	db     *Database
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
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

	// Trying to create test.db with no access rights
	// Check fail of the createConfigTable
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

func TestAosVersion(t *testing.T) {
	var setAosVersion uint64 = 53

	if err := db.SetAosVersion("id0", setAosVersion); err != nil {
		t.Fatalf("Can't set Aos version: %s", err)
	}

	getAosVersion, err := db.GetAosVersion("id0")
	if err != nil {
		t.Fatalf("Can't get Aos version: %s", err)
	}

	if setAosVersion != getAosVersion {
		t.Fatalf("Wrong Aos version: %v", getAosVersion)
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

func TestMultiThread(t *testing.T) {
	const numIterations = 1000

	var wg sync.WaitGroup

	wg.Add(2)

	var testErr error

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if err := db.SetUpdateState([]byte(strconv.Itoa(i))); err != nil {
				if testErr == nil {
					testErr = err
				}

				return
			}
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < numIterations; i++ {
			if _, err := db.GetUpdateState(); err != nil {
				if testErr == nil {
					testErr = err
				}

				return
			}
		}
	}()

	wg.Wait()
}

func TestMigrationToV1(t *testing.T) {
	migrationDB := path.Join(tmpDir, "test_migration.db")

	if err := os.RemoveAll(migrationDB); err != nil {
		t.Fatalf("Error deleting migration DB: %s", err)
	}

	if err := createDatabaseV0(migrationDB); err != nil {
		t.Fatalf("Can't create initial database: %s", err)
	}

	// Migration upward
	db, err := newDatabase(migrationDB, migrationDir, migrationDir, 1)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer1(db.sql); err != nil {
		t.Fatalf("Error checking db version: %s", err)
	}

	db.Close()

	// Migration downward
	db, err = newDatabase(migrationDB, migrationDir, migrationDir, 0)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer0(db.sql); err != nil {
		t.Fatalf("Error checking db version: %s", err)
	}

	db.Close()
}

func TestMigrationToV2(t *testing.T) {
	migrationDB := path.Join(tmpDir, "test_migration.db")

	if err := os.RemoveAll(migrationDB); err != nil {
		t.Fatalf("Error deleting migration DB: %s", err)
	}

	if err := createDatabaseV1(migrationDB); err != nil {
		t.Fatalf("Can't create initial database: %s", err)
	}

	// Migration upward
	db, err := newDatabase(migrationDB, migrationDir, migrationDir, 2)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer2(db.sql); err != nil {
		t.Fatalf("Error checking db version: %s", err)
	}

	db.Close()

	// Migration downward
	db, err = newDatabase(migrationDB, "migration", "mergedMigration", 1)
	if err != nil {
		t.Fatalf("Can't create database: %s", err)
	}

	if err = isDatabaseVer1(db.sql); err != nil {
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
		return aoserrors.Wrap(err)
	}
	defer sqlite.Close()

	if _, err = sqlite.Exec(`CREATE TABLE config (
			updateState TEXT,
			version INTEGER)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`INSERT INTO config (updateState, version) values(?, ?)`, "", 0); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS modules (
		id TEXT NOT NULL PRIMARY KEY,
		state TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS modules_data (
		id TEXT NOT NULL PRIMARY KEY,
		name TEXT NOT NULL,
		value TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS certificates (
		type TEXT NOT NULL,
		issuer TEXT NOT NULL,
		serial TEXT NOT NULL,
		crtURL TEXT,
		keyURL TEXT,
		notAfter TIMESTAMP,
		PRIMARY KEY (issuer, serial))`); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = migration.SetDatabaseVersion(sqlite, migrationDir, 0); err != nil {
		return aoserrors.Errorf("%s (%s)", ErrMigrationFailedStr, err.Error())
	}

	return nil
}

func createDatabaseV1(name string) (err error) {
	sqlite, err := sql.Open("sqlite3", fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=%s&_sync=%s",
		name, busyTimeout, journalMode, syncMode))
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer sqlite.Close()

	if _, err = sqlite.Exec(`CREATE TABLE config (
			updateState TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`INSERT INTO config (updateState) values(?)`, ""); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`CREATE TABLE IF NOT EXISTS modules (
		id TEXT NOT NULL PRIMARY KEY,
		vendorVersion TEXT,
		aosVersion INTEGER,
		state TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = sqlite.Exec(`INSERT INTO modules (id, vendorVersion, aosVersion, state)
		values(?, ?, ? ,?)`, "id1", "1.0", 3, "module state"); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = migration.SetDatabaseVersion(sqlite, migrationDir, 1); err != nil {
		return aoserrors.Errorf("%s (%s)", ErrMigrationFailedStr, err.Error())
	}

	return nil
}

func isDatabaseVer0(sqlite *sql.DB) (err error) {
	if err = checkColumn(sqlite, "config", "version"); err != nil {
		return aoserrors.Wrap(err)
	}

	rows, err := sqlite.Query("SELECT version FROM config")
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return aoserrors.Wrap(rows.Err())
	}

	if !rows.Next() {
		return aoserrors.New(ErrNotExistStr)
	}

	var version int

	if err = rows.Scan(&version); err != nil {
		return aoserrors.Wrap(err)
	}

	if version != 5 {
		return aoserrors.Errorf("wrong version in database: expected 5, got %d", version)
	}

	return nil
}

func isDatabaseVer1(sqlite *sql.DB) (err error) {
	if err = checkColumn(sqlite, "config", "version"); err == nil {
		return aoserrors.New("column `version` in `config` should not exist")
	}

	if err = checkColumn(sqlite, "modules", "vendorVersion"); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func isDatabaseVer2(sqlite *sql.DB) (err error) {
	if err = checkColumn(sqlite, "modules", "vendorVersion"); err == nil {
		return aoserrors.New("column `vendorVersion` in `modules` should not exist")
	}

	rows, err := sqlite.Query("SELECT id, aosVersion, state FROM modules")
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return aoserrors.Wrap(rows.Err())
	}

	if !rows.Next() {
		return aoserrors.New(ErrNotExistStr)
	}

	var (
		id, state  string
		aosVersion int
	)

	if err = rows.Scan(&id, &aosVersion, &state); err != nil {
		return aoserrors.Wrap(err)
	}

	if id != "id1" || aosVersion != 3 || state != "module state" {
		return aoserrors.New("wrong module values")
	}

	return nil
}

func checkColumn(sqlite *sql.DB, table, column string) (err error) {
	rows, err := sqlite.Query("SELECT COUNT(*) AS CNTREC FROM pragma_table_info(?) WHERE name=?", table, column)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return aoserrors.Wrap(rows.Err())
	}

	if !rows.Next() {
		return aoserrors.New(ErrNotExistStr)
	}

	var count int

	if err = rows.Scan(&count); err != nil {
		return aoserrors.Wrap(err)
	}

	if count == 0 {
		return aoserrors.New(ErrNotExistStr)
	}

	return nil
}
