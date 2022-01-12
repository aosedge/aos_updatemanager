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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/migration"
	_ "github.com/mattn/go-sqlite3" // ignore lint
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	busyTimeout = 60000
	journalMode = "WAL"
	syncMode    = "NORMAL"
)

const dbVersion = 2

/*******************************************************************************
 * Vars
 ******************************************************************************/

// ErrNotExistStr is returned when requested entry not exist in DB.
var ErrNotExistStr = "entry doesn't exist"

// ErrMigrationFailedStr is returned if migration was failed and db returned to the previous state.
var ErrMigrationFailedStr = "database migration failed"

/*******************************************************************************
 * Types
 ******************************************************************************/

// Database structure with database information.
type Database struct {
	sql *sql.DB
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new database handle.
func New(name string, migrationPath string, mergedMigrationPath string) (db *Database, err error) {
	if db, err = newDatabase(name, migrationPath, mergedMigrationPath, dbVersion); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return db, nil
}

// SetUpdateState stores update state.
func (db *Database) SetUpdateState(state []byte) (err error) {
	result, err := db.sql.Exec("UPDATE config SET updateState = ?", state)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if count == 0 {
		return aoserrors.New(ErrNotExistStr)
	}

	return nil
}

// GetUpdateState returns update state.
func (db *Database) GetUpdateState() (state []byte, err error) {
	stmt, err := db.sql.Prepare("SELECT updateState FROM config")
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, aoserrors.New(ErrNotExistStr)
		}

		return nil, aoserrors.Wrap(err)
	}

	return state, nil
}

// GetModuleState returns module state.
func (db *Database) GetModuleState(id string) (state []byte, err error) {
	rows, err := db.sql.Query("SELECT state FROM modules WHERE id = ?", id)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return nil, aoserrors.Wrap(rows.Err())
	}

	if !rows.Next() {
		return nil, aoserrors.New(ErrNotExistStr)
	}

	if err = rows.Scan(&state); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return state, nil
}

// SetModuleState sets module state.
func (db *Database) SetModuleState(id string, state []byte) (err error) {
	result, err := db.sql.Exec("UPDATE modules SET state = ? WHERE id= ?", state, id)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if count == 0 {
		if _, err = db.sql.Exec("INSERT INTO modules (id, state) values(?, ?)", id, state); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

// GetAosVersion returns module Aos version.
func (db *Database) GetAosVersion(id string) (version uint64, err error) {
	rows, err := db.sql.Query("SELECT aosVersion FROM modules WHERE id = ?", id)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return 0, aoserrors.Wrap(rows.Err())
	}

	if !rows.Next() {
		return 0, aoserrors.New(ErrNotExistStr)
	}

	if err = rows.Scan(&version); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return version, nil
}

// SetAosVersion sets module Aos version.
func (db *Database) SetAosVersion(id string, version uint64) (err error) {
	result, err := db.sql.Exec("UPDATE modules SET aosVersion = ? WHERE id= ?", version, id)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if count == 0 {
		if _, err = db.sql.Exec("INSERT INTO modules (id, aosVersion) values(?, ?)", id, version); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

// Close closes database.
func (db *Database) Close() {
	db.sql.Close()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newDatabase(
	name string, migrationPath string, mergedMigrationPath string, version uint) (db *Database, err error) {
	log.WithField("name", name).Debug("Open database")

	// Check and create db path
	if _, err = os.Stat(filepath.Dir(name)); err != nil {
		if !os.IsNotExist(err) {
			return db, aoserrors.Wrap(err)
		}

		if err = os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return db, aoserrors.Wrap(err)
		}
	}

	sqlite, err := sql.Open("sqlite3", fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=%s&_sync=%s",
		name, busyTimeout, journalMode, syncMode))
	if err != nil {
		return db, aoserrors.Wrap(err)
	}

	db = &Database{sqlite}

	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	if migrationPath != mergedMigrationPath {
		if err = migration.MergeMigrationFiles(migrationPath, mergedMigrationPath); err != nil {
			return db, aoserrors.Wrap(err)
		}
	}

	exists, err := db.isTableExist("config")
	if err != nil {
		return db, aoserrors.Wrap(err)
	}

	if !exists {
		// Set database version if database not exist
		if err = migration.SetDatabaseVersion(sqlite, mergedMigrationPath, version); err != nil {
			return db, aoserrors.Errorf("%s (%s)", ErrMigrationFailedStr, err.Error())
		}
	} else {
		if err = migration.DoMigrate(db.sql, mergedMigrationPath, version); err != nil {
			return db, aoserrors.Errorf("%s (%s)", ErrMigrationFailedStr, err.Error())
		}
	}

	if err = db.createConfigTable(); err != nil {
		return db, err
	}

	if err := db.createModuleTable(); err != nil {
		return db, aoserrors.Wrap(err)
	}

	return db, nil
}

func (db *Database) isTableExist(name string) (result bool, err error) {
	rows, err := db.sql.Query("SELECT * FROM sqlite_master WHERE name = ? and type='table'", name)
	if err != nil {
		return false, aoserrors.Wrap(err)
	}
	defer rows.Close()

	result = rows.Next()

	return result, aoserrors.Wrap(rows.Err())
}

func (db *Database) createConfigTable() (err error) {
	log.Info("Create config table")

	exist, err := db.isTableExist("config")
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if exist {
		return nil
	}

	if _, err = db.sql.Exec(
		`CREATE TABLE config (
			updateState TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = db.sql.Exec(
		`INSERT INTO config (
			updateState) values(?)`, ""); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (db *Database) createModuleTable() (err error) {
	log.Info("Create module table")

	if _, err = db.sql.Exec(
		`CREATE TABLE IF NOT EXISTS modules (
			id TEXT NOT NULL PRIMARY KEY,
			aosVersion INTEGER,
			state TEXT)`); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
