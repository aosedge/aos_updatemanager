package main

import (
	"io/ioutil"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Var
 ******************************************************************************/

var module updatehandler.Module

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
		"DestPath": "remoteTestFile",
		"Commands":[
			"cd . ",
			"pwd",
			"ls"
		]
	}`

	module, err = NewModule("TestModule", []byte(configJSON))
	if err != nil {
		log.Fatalf("Can't create SSH module: %s", err)
	}

	ret := m.Run()

	module.Close()

	os.Exit(ret)

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id := module.GetID()
	if id != "TestModule" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestUpgrade(t *testing.T) {
	if err := ioutil.WriteFile("tmp/testfile", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp/testfile"); err != nil {
		t.Errorf("Upgrade failed: %s", err)
	}
}
