package statecontroller_test

import (
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/statecontroller"
)

/*******************************************************************************
 * Var
 ******************************************************************************/

var controller *statecontroller.Controller

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

	if controller, err = statecontroller.New(nil); err != nil {
		log.Fatalf("Error creating state controller: %s", err)
	}

	ret := m.Run()

	controller.Close()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetVersion(t *testing.T) {
	if _, err := controller.GetVersion(); err != nil {
		t.Errorf("Can't get system version: %s", err)
	}
}
