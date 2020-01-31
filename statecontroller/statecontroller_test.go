package statecontroller_test

import (
	"errors"
	"io/ioutil"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/statecontroller"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testModuleMgr struct {
	modules map[string]interface{}
}

type testUpdateModule struct {
	path   string
	fsType string
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var controller *statecontroller.Controller

var moduleMgr = testModuleMgr{
	modules: map[string]interface{}{
		"rootfs":     &testUpdateModule{},
		"bootloader": &testUpdateModule{}},
}

var configJSON = `
{
	"KernelCmdline" : "tmp/cmdline",
	"BootPartitions" : [
		{
			"device" : "/dev/myBoot1",
			"fstype" : "vfat"
		},
		{
			"device" : "/dev/myBoot2",
			"fstype" : "vfat"
		}
	],
	"RootPartitions" : [
		{
			"device" : "/dev/myRoot1",
			"fstype" : "ext4"
		},
		{
			"device" : "/dev/myRoot2",
			"fstype" : "ext4"
		}
	]
}`

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
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	if err := createCmdLine("opt1=val1 root=/dev/myRoot1 opt2=val2 NUANCE.boot=(hd0,gpt1)/EFI/BOOT"); err != nil {
		log.Fatalf("Can't create cmdline file: %s", err)
	}

	ret := m.Run()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetVersion(t *testing.T) {
	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr)
	if err != nil {
		log.Fatalf("Error creating state controller: %s", err)
	}
	defer controller.Close()

	if _, err := controller.GetVersion(); err != nil {
		t.Errorf("Can't get system version: %s", err)
	}
}

func TestCheckUpdatePartition(t *testing.T) {
	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr)
	if err != nil {
		log.Fatalf("Error creating state controller: %s", err)
	}
	defer controller.Close()

	if err = controller.Upgrade(1, []string{"rootfs", "bootloader"}); err != nil {
		t.Fatalf("Can't upgrade state: %s", err)
	}

	// Check rootFS module

	testModule, err := getTestModule("rootfs")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != "/dev/myRoot2" {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != "ext4" {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

	// Check bootloader module

	if testModule, err = getTestModule("bootloader"); err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != "/dev/myBoot2" {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != "vfat" {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

func (mgr *testModuleMgr) GetModuleByID(id string) (module interface{}, err error) {
	testModule, ok := mgr.modules[id]
	if !ok {
		return nil, errors.New("module not found")
	}

	return testModule, nil
}

func (module *testUpdateModule) SetPartitionForUpdate(path, fsType string) (err error) {
	module.path = path
	module.fsType = fsType

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createCmdLine(data string) (err error) {
	if err = ioutil.WriteFile("tmp/cmdline", []byte(data), 0644); err != nil {
		return err
	}

	return nil
}

func getTestModule(id string) (testModule *testUpdateModule, err error) {
	module, err := moduleMgr.GetModuleByID(id)
	if err != nil {
		return nil, err
	}

	var ok bool

	if testModule, ok = module.(*testUpdateModule); !ok {
		return nil, errors.New("wrong module type")
	}

	return testModule, nil
}
