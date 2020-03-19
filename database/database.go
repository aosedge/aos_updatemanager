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
	"errors"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" //ignore lint
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	dbVersion = 3
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

// ErrNotExist is returned when requested entry not exist in DB
var ErrNotExist = errors.New("entry doesn't not exist")

// ErrVersionMismatch is returned when DB has unsupported DB version
var ErrVersionMismatch = errors.New("version mismatch")

/*******************************************************************************
 * Types
 ******************************************************************************/

// Database structure with database information
type Database struct {
	sql *sql.DB
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new database handle
func New(name string) (db *Database, err error) {
	log.WithField("name", name).Debug("Open database")

	// Check and create db path
	if _, err = os.Stat(filepath.Dir(name)); err != nil {
		if !os.IsNotExist(err) {
			return db, err
		}
		if err = os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return db, err
		}
	}

	var sqlite *sql.DB

	sqlite, err = sql.Open("sqlite3", name)
	if err != nil {
		return db, err
	}

	defer func() {
		if err != nil {
			sqlite.Close()
		}
	}()

	db = &Database{sqlite}

	if err = db.createConfigTable(); err != nil {
		return db, err
	}

	if err = db.createModuleStatusesTable(); err != nil {
		return db, err
	}

	version, err := db.getVersion()
	if err != nil {
		return db, err
	}

	if version != dbVersion {
		return db, ErrVersionMismatch
	}

	return db, nil
}

// SetOperationState stores operation state
func (db *Database) SetOperationState(state []byte) (err error) {
	result, err := db.sql.Exec("UPDATE config SET operationState = ?", state)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if count == 0 {
		return ErrNotExist
	}

	return nil
}

// GetOperationState returns operation state
func (db *Database) GetOperationState() (state []byte, err error) {
	stmt, err := db.sql.Prepare("SELECT operationState FROM config")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&state)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotExist
		}

		return nil, err
	}

	return state, nil
}

// AddModuleStatus adds module status to DB
func (db *Database) AddModuleStatus(id string, status error) (err error) {
	statusStr := ""

	if status != nil {
		statusStr = status.Error()
	}

	if _, err = db.sql.Exec("INSERT INTO moduleStatuses VALUES(?, ?)", id, statusStr); err != nil {
		return err
	}

	return nil
}

// RemoveModuleStatus removes module status from DB
func (db *Database) RemoveModuleStatus(id string) (err error) {
	if _, err = db.sql.Exec("DELETE FROM moduleStatuses WHERE module = ?", id); err != nil {
		return err
	}

	return nil
}

// GetModuleStatuses returns all added module statuses
func (db *Database) GetModuleStatuses() (moduleStatuses map[string]error, err error) {
	rows, err := db.sql.Query("SELECT * FROM moduleStatuses")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	moduleStatuses = make(map[string]error)

	for rows.Next() {
		var id, statusStr string

		if err = rows.Scan(&id, &statusStr); err != nil {
			return nil, err
		}

		var status error

		if statusStr != "" {
			status = errors.New(statusStr)
		}

		moduleStatuses[id] = status
	}

	return moduleStatuses, rows.Err()
}

// ClearModuleStatuses clears module statuses DB
func (db *Database) ClearModuleStatuses() (err error) {
	_, err = db.sql.Exec("DELETE FROM moduleStatuses")

	return err
}

// Close closes database
func (db *Database) Close() {
	db.sql.Close()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (db *Database) getVersion() (version uint64, err error) {
	stmt, err := db.sql.Prepare("SELECT version FROM config")
	if err != nil {
		return version, err
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return version, ErrNotExist
		}

		return version, err
	}

	return version, nil
}

func (db *Database) setVersion(version uint64) (err error) {
	result, err := db.sql.Exec("UPDATE config SET version = ?", version)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if count == 0 {
		return ErrNotExist
	}

	return nil
}

func (db *Database) isTableExist(name string) (result bool, err error) {
	rows, err := db.sql.Query("SELECT * FROM sqlite_master WHERE name = ? and type='table'", name)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	result = rows.Next()

	return result, rows.Err()
}

func (db *Database) createConfigTable() (err error) {
	log.Info("Create config table")

	exist, err := db.isTableExist("config")
	if err != nil {
		return err
	}

	if exist {
		return nil
	}

	if _, err = db.sql.Exec(
		`CREATE TABLE config (
			version INTEGER,
			operationState TEXT)`); err != nil {
		return err
	}

	if _, err = db.sql.Exec(
		`INSERT INTO config (
			version,
			operationState) values(?, ?)`, dbVersion, ""); err != nil {
		return err
	}

	return nil
}

func (db *Database) createModuleStatusesTable() (err error) {
	log.Info("Create module status table")

	exist, err := db.isTableExist("moduleStatuses")
	if err != nil {
		return err
	}

	if exist {
		return nil
	}

	if _, err = db.sql.Exec(
		`CREATE TABLE moduleStatuses (
			module TEXT NOT NULL PRIMARY KEY,
			status TEXT)`); err != nil {
		return err
	}

	return nil
}
