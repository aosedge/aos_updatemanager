package database

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/umserver"

	log "github.com/sirupsen/logrus"
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
	setState := umserver.RevertingState
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

func TestFilesInfo(t *testing.T) {
	setFilesInfo := []umserver.UpgradeFileInfo{
		umserver.UpgradeFileInfo{
			Target: "target",
			URL:    "url1",
			Sha256: []byte{1, 2, 3, 4, 5, 6},
			Sha512: []byte{1, 2, 3, 4, 5, 6},
			Size:   1234}}
	if err := db.SetFilesInfo(setFilesInfo); err != nil {
		t.Fatalf("Can't set files info: %s", err)
	}

	getFilesInfo, err := db.GetFilesInfo()
	if err != nil {
		t.Fatalf("Can't get files info: %s", err)
	}

	if !reflect.DeepEqual(setFilesInfo, getFilesInfo) {
		t.Fatalf("Wrong files info value: %v", getFilesInfo)
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
