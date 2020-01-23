package statecontroller_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	modules: map[string]interface{}{"rootfs": &testUpdateModule{}},
}

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
	configJSON := `
{
	"RootPartitions" : [
		{
			"device" : "/dev/myPart1",
			"fstype" : "ext4"
		},
		{
			"device" : "/dev/myPart2",
			"fstype" : "ext4"
		}
	]
}`

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
	configJSON := `
{
	"RootPartitions" : [
		{
			"device" : "%s",
			"fstype" : "ext4"
		},
		{
			"device" : "%s",
			"fstype" : "ext4"
		}
	]
}`

	path, err := mountPartition()
	if err != nil {
		t.Fatalf("Can't mount rootfs: %s", err)
	}
	defer unmountPartition()

	controller, err := statecontroller.New([]byte(fmt.Sprintf(configJSON, path, "/dev/myPart2")), &moduleMgr)
	if err != nil {
		log.Fatalf("Error creating state controller: %s", err)
	}
	defer controller.Close()

	module, err := moduleMgr.GetModuleByID("rootfs")
	if err != nil {
		t.Fatalf("Can't get rootfs module: %s", err)
	}

	testModule, ok := module.(*testUpdateModule)
	if !ok {
		t.Fatal("Wrong module type")
	}

	if testModule.path != "/dev/myPart2" {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != "ext4" {
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

func mountPartition() (device string, err error) {
	if err = os.MkdirAll("tmp/mount", 0755); err != nil {
		return "", err
	}

	if err = exec.Command("dd", "if=/dev/zero", "of=tmp/test.fs", "bs=1024", "count=1024").Run(); err != nil {
		return "", err
	}

	if err = exec.Command("mkfs.ext4", "tmp/test.fs").Run(); err != nil {
		return "", err
	}

	if err = exec.Command("mount", "tmp/test.fs", "tmp/mount").Run(); err != nil {
		return "", err
	}

	blkInfo := struct {
		BlockDevices []struct {
			Name       string
			Mountpoint string
		}
	}{}

	outJSON, err := exec.Command("lsblk", "-J").Output()
	if err != nil {
		return "", err
	}

	if err = json.Unmarshal(outJSON, &blkInfo); err != nil {
		return "", err
	}

	mountPoint, err := filepath.Abs("tmp/mount")
	if err != nil {
		return "", err
	}

	for _, device := range blkInfo.BlockDevices {
		if device.Mountpoint == mountPoint {
			return path.Join("/dev", device.Name), nil
		}
	}

	return "", errors.New("loop device not found")
}

func unmountPartition() {
	if err := exec.Command("umount", "tmp/mount").Run(); err != nil {
		log.Errorf("Can't unmount partition: %s", err)
	}
}
