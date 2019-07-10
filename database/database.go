package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" //ignore lint
	log "github.com/sirupsen/logrus"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/umserver"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	dbVersion = 1
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

	sqlite, err := sql.Open("sqlite3", name)
	if err != nil {
		return db, err
	}

	db = &Database{sqlite}

	if err := db.createConfigTable(); err != nil {
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

// SetState stores state
func (db *Database) SetState(state int) (err error) {
	result, err := db.sql.Exec("UPDATE config SET state = ?", state)
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

// GetState returns state
func (db *Database) GetState() (state int, err error) {
	stmt, err := db.sql.Prepare("SELECT state FROM config")
	if err != nil {
		return state, err
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&state)
	if err != nil {
		if err == sql.ErrNoRows {
			return state, ErrNotExist
		}

		return state, err
	}

	return state, nil
}

// SetFilesInfo stores files info
func (db *Database) SetFilesInfo(filesInfo []umserver.UpgradeFileInfo) (err error) {
	infoJSON, err := json.Marshal(&filesInfo)
	if err != nil {
		return err
	}

	result, err := db.sql.Exec("UPDATE config SET filesInfo = ?", infoJSON)
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

// GetFilesInfo returns files info
func (db *Database) GetFilesInfo() (filesInfo []umserver.UpgradeFileInfo, err error) {
	stmt, err := db.sql.Prepare("SELECT filesInfo FROM config")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var infoJSON []byte

	if err = stmt.QueryRow().Scan(&infoJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotExist
		}

		return nil, err
	}

	if infoJSON == nil {
		return nil, nil
	}

	if err = json.Unmarshal(infoJSON, &filesInfo); err != nil {
		return nil, err
	}

	return filesInfo, nil
}

// SetOperationVersion stores operation version
func (db *Database) SetOperationVersion(version uint64) (err error) {
	result, err := db.sql.Exec("UPDATE config SET operationVersion = ?", version)
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

// GetOperationVersion returns upgrade version
func (db *Database) GetOperationVersion() (version uint64, err error) {
	stmt, err := db.sql.Prepare("SELECT operationVersion FROM config")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	if err = stmt.QueryRow().Scan(&version); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotExist
		}

		return 0, err
	}

	return version, nil
}

// SetLastError stores last error
func (db *Database) SetLastError(lastError error) (err error) {
	result, err := db.sql.Exec("UPDATE config SET lastError = ?", lastError.Error())
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

// GetLastError returns last error
func (db *Database) GetLastError() (lastError error, err error) {
	stmt, err := db.sql.Prepare("SELECT lastError FROM config")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	errorStr := ""

	if err = stmt.QueryRow().Scan(&errorStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotExist
		}

		return nil, err
	}

	return errors.New(errorStr), nil
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
			state INTEGER,
			operationVersion INTEGER,
			filesInfo BLOB,
			lastError TEXT)`); err != nil {
		return err
	}

	if _, err = db.sql.Exec(
		`INSERT INTO config (
			version,
			state,
			operationVersion,
			filesInfo,
			lastError) values(?, ?, ?, ?, ?)`, dbVersion, umserver.UpgradedState, 0, []byte{}, ""); err != nil {
		return err
	}

	return nil
}
