package statecontroller_test

import (
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	fsmodule "aos_updatemanager/modulemanager/fsmodule"
	"aos_updatemanager/statecontroller"
)

/*******************************************************************************
 * Types
 ******************************************************************************/
type TestModuleMgr struct {
}

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

	moduleMgr := TestModuleMgr{}

	if controller, err = statecontroller.New(nil, &moduleMgr); err != nil {
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

func TestUpgradeWithRootfs(t *testing.T) {
	if err := controller.Upgrade(10); err != nil {
		t.Errorf("Can't call Upgrade: %s", err)
	}
}

func (mgr *TestModuleMgr) GetModuleByID(id string) (module interface{}, err error) {
	//TODO: do not return new module each call GetModuleByID.
	return fsmodule.New("rootfs", nil)
}
