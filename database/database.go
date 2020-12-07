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
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" //ignore lint
	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/migration"

	"aos_updatemanager/crthandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	busyTimeout = 60000
	journalMode = "WAL"
	syncMode    = "NORMAL"
)

const dbVersion = 1

/*******************************************************************************
 * Vars
 ******************************************************************************/

// ErrNotExist is returned when requested entry not exist in DB
var ErrNotExist = errors.New("entry doesn't not exist")

// ErrMigrationFailed is returned if migration was failed and db returned to the previous state
var ErrMigrationFailed = errors.New("database migration failed")

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
func New(name string, migrationPath string, mergedMigrationPath string) (db *Database, err error) {
	return newDatabase(name, migrationPath, mergedMigrationPath, dbVersion)
}

// SetUpdateState stores update state
func (db *Database) SetUpdateState(state []byte) (err error) {
	result, err := db.sql.Exec("UPDATE config SET updateState = ?", state)
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

// GetUpdateState returns update state
func (db *Database) GetUpdateState() (state []byte, err error) {
	stmt, err := db.sql.Prepare("SELECT updateState FROM config")
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

// GetModuleState returns module state
func (db *Database) GetModuleState(id string) (state []byte, err error) {
	rows, err := db.sql.Query("SELECT state FROM modules WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		if err = rows.Scan(&state); err != nil {
			return nil, err
		}

		return state, nil
	}

	return nil, ErrNotExist
}

// SetModuleState sets module state
func (db *Database) SetModuleState(id string, state []byte) (err error) {
	result, err := db.sql.Exec("REPLACE INTO modules (id, state) VALUES(?, ?)", id, state)
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

// GetControllerState returns controller state
func (db *Database) GetControllerState(controllerID string, name string) (value []byte, err error) {
	rows, err := db.sql.Query("SELECT value FROM modules_data WHERE id = ? and name = ?", controllerID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}

		return value, nil
	}

	return nil, ErrNotExist
}

// SetControllerState sets controller state
func (db *Database) SetControllerState(controllerID string, name string, value []byte) (err error) {
	result, err := db.sql.Exec("REPLACE INTO modules_data (id, name, value) VALUES(?, ?, ?)", controllerID,
		name, value)
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

// AddCertificate adds new certificate to database
func (db *Database) AddCertificate(crtType string, crt crthandler.CrtInfo) (err error) {
	if _, err = db.sql.Exec("INSERT INTO certificates values(?, ?, ?, ?, ?, ?)",
		crtType, crt.Issuer, crt.Serial, crt.CrtURL, crt.KeyURL, crt.NotAfter); err != nil {
		return err
	}

	return nil
}

// GetCertificate returns certificate by issuer and serial
func (db *Database) GetCertificate(issuer, serial string) (crt crthandler.CrtInfo, err error) {
	rows, err := db.sql.Query("SELECT issuer, serial, crtURL, keyURL, notAfter FROM certificates WHERE issuer = ? AND serial = ?",
		issuer, serial)
	if err != nil {
		return crt, err
	}
	defer rows.Close()

	for rows.Next() {
		if err = rows.Scan(&crt.Issuer, &crt.Serial, &crt.CrtURL, &crt.KeyURL, &crt.NotAfter); err != nil {
			return crt, err
		}

		return crt, nil
	}

	return crt, ErrNotExist
}

// GetCertificates returns certificates of selected type
func (db *Database) GetCertificates(crtType string) (crts []crthandler.CrtInfo, err error) {
	rows, err := db.sql.Query("SELECT issuer, serial, crtURL, keyURL, notAfter FROM certificates WHERE type = ?", crtType)
	if err != nil {
		return crts, err
	}
	defer rows.Close()

	for rows.Next() {
		var crt crthandler.CrtInfo

		if err = rows.Scan(&crt.Issuer, &crt.Serial, &crt.CrtURL, &crt.KeyURL, &crt.NotAfter); err != nil {
			return crts, err
		}

		crts = append(crts, crt)
	}

	return crts, nil
}

// RemoveCertificate removes certificate from database
func (db *Database) RemoveCertificate(crtType, crtURL string) (err error) {
	if _, err = db.sql.Exec("DELETE FROM certificates WHERE type = ? AND crtURL = ?", crtType, crtURL); err != nil {
		return err
	}

	return nil
}

// Close closes database
func (db *Database) Close() {
	db.sql.Close()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newDatabase(name string, migrationPath string, mergedMigrationPath string, version uint) (db *Database, err error) {
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

	sqlite, err := sql.Open("sqlite3", fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=%s&_sync=%s",
		name, busyTimeout, journalMode, syncMode))
	if err != nil {
		return db, err
	}

	db = &Database{sqlite}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	if err = migration.MergeMigrationFiles(migrationPath, mergedMigrationPath); err != nil {
		return db, err
	}

	exists, err := db.isTableExist("config")
	if err != nil {
		return db, err
	}

	if !exists {
		// Set database version if database not exist
		if err = migration.SetDatabaseVersion(sqlite, migrationPath, version); err != nil {
			log.Debugf("Error forcing database version. Err: %s", err)
			return db, ErrMigrationFailed
		}
	} else {
		if err = migration.DoMigrate(db.sql, mergedMigrationPath, version); err != nil {
			log.Debugf("Error during database migration. Err: %s", err)
			return db, ErrMigrationFailed
		}
	}

	if err = db.createConfigTable(); err != nil {
		return db, err
	}

	if err := db.createModuleTable(); err != nil {
		return db, err
	}

	if err := db.createModulesDataTable(); err != nil {
		return db, err
	}

	if err := db.createCertTable(); err != nil {
		return db, err
	}

	return db, nil

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
			updateState TEXT)`); err != nil {
		return err
	}

	if _, err = db.sql.Exec(
		`INSERT INTO config (
			updateState) values(?)`, ""); err != nil {
		return err
	}

	return nil
}

func (db *Database) createModuleTable() (err error) {
	log.Info("Create module table")

	if _, err = db.sql.Exec(`CREATE TABLE IF NOT EXISTS modules (id TEXT NOT NULL PRIMARY KEY, state TEXT)`); err != nil {
		return err
	}

	return nil
}

func (db *Database) createModulesDataTable() (err error) {
	log.Info("Create modules_data table")

	if _, err = db.sql.Exec(`CREATE TABLE IF NOT EXISTS modules_data (id TEXT NOT NULL PRIMARY KEY, name TEXT NOT NULL, value TEXT)`); err != nil {
		return err
	}

	return nil
}

func (db *Database) createCertTable() (err error) {
	log.Info("Create cert table")

	if _, err = db.sql.Exec(`CREATE TABLE IF NOT EXISTS certificates (
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
