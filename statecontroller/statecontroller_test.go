package statecontroller_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/statecontroller"
	"aos_updatemanager/utils/testtools"
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

var wrongModuleMgr = testModuleMgr{
	modules: map[string]interface{}{
		"wrongfs":    &testUpdateModule{},
		"bootloader": &testUpdateModule{}},
}

var notImpModuleMgr = testModuleMgr{
	modules: map[string]interface{}{
		"rootfs":     nil,
		"bootloader": &testUpdateModule{}},
}

var configJSON = `
{
	"KernelCmdline" : "tmp/cmdline",
	"StateFile" : "tmp/state",
	"BootPartitions" : [
		{
			"device" : "/dev/boot",
			"fstype" : "vfat"
		},
		{
			"device" : "/dev/boot",
			"fstype" : "vfat"
		}
	],
	"RootPartitions" : [
		{
			"device" : "/dev/root1",
			"fstype" : "ext4"
		},
		{
			"device" : "/dev/root2",
			"fstype" : "ext4"
		}
	]
}`

var bootParts = []string{"", ""}

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
	if err := os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	for i := 0; i < 2; i++ {
		device, err := createBootPartition("tmp/boot" + strconv.Itoa(i))
		if err != nil {
			log.Fatalf("Can't create boot partition: %s", err)
		}

		configJSON = strings.Replace(configJSON, "/dev/boot", device, 1)

		bootParts[i] = device
	}

	ret := m.Run()

	for _, bootPart := range bootParts {
		if err := deleteBootPartition(bootPart); err != nil {
			log.Errorf("Can't delete test boot part: %s", err)
		}
	}

	if err := os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestCheckUpdateRootfs(t *testing.T) {
	if err := createCmdLine(bootParts[0], "/dev/root1", 0, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr)
	if err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}
	defer func() {
		if err := controller.Close(); err != nil {
			t.Errorf("Error closing state controller: %s", err)
		}
	}()

	// Start upgrade rootfs

	if err = controller.Upgrade(1, map[string]string{"rootfs": "/path/to/upgrade"}); err != nil {
		t.Fatalf("Can't upgrade: %s", err)
	}

	testModule, err := getTestModule("rootfs")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != "/dev/root2" {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != "ext4" {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

	// Upgrade finish rootfs

	postpone, err := controller.UpgradeFinished(1, nil, map[string]error{"rootfs": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}

	if !postpone {
		t.Fatal("Upgrade should be postponed")
	}

	env, err := readEnvVariables(bootParts[0])
	if err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check that rootfs switch scheduled

	trySwitch, ok := env["NUANCE_TRY_SWITCH"]
	if !ok {
		t.Errorf("NUANCE_TRY_SWITCH variable is not set")
	}
	if trySwitch != "1" {
		t.Errorf("Wrong NUANCE_TRY_SWITCH value: %s", trySwitch)
	}

	if err = setEnvVariables(bootParts[0], map[string]string{"NUANCE_TRY_SWITCH": "0"}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	// Reboot

	if err := controller.Close(); err != nil {
		t.Errorf("Error closing state controller: %s", err)
	}

	if err := createCmdLine(bootParts[0], "/dev/root2", 1, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	if controller, err = statecontroller.New([]byte(configJSON), &moduleMgr); err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}

	postpone, err = controller.UpgradeFinished(1, nil, map[string]error{"rootfs": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}
	if postpone {
		t.Errorf("Upgrade should not be postponed")
	}

	if env, err = readEnvVariables(bootParts[0]); err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check version

	version, ok := env["NUANCE_VERSION"]
	if !ok {
		t.Errorf("NUANCE_VERSION variable is not set")
	}
	if version != "1" {
		t.Errorf("Wrong NUANCE_VERSION value: %s", version)
	}

	// Check active boot index

	bootIndex, ok := env["NUANCE_ACTIVE_BOOT_INDEX"]
	if !ok {
		t.Errorf("NUANCE_ACTIVE_BOOT_INDEX variable is not set")
	}
	if bootIndex != "1" {
		t.Errorf("Wrong NUANCE_ACTIVE_BOOT_INDEX value: %s", bootIndex)
	}

	// Check that second partition is updated

	if testModule.path != "/dev/root1" {
		t.Errorf("Second root FS partition wasn't updated")
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

func (module *testUpdateModule) Upgrade(path string) (err error) {
	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createCmdLine(bootDevice, rootDevice string, bootIndex int, version uint64) (err error) {
	if err = ioutil.WriteFile("tmp/cmdline",
		[]byte(fmt.Sprintf("root=%s NUANCE.bootDevice=(hd0,gpt%s)/EFI/BOOT NUANCE.bootIndex=%d NUANCE.version=%d",
			rootDevice,
			regexp.MustCompile("[[:digit:]]*$").FindString(bootDevice),
			bootIndex,
			version)), 0644); err != nil {
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

func createBootPartition(path string) (device string, err error) {
	if err = os.MkdirAll("tmp/mount", 0755); err != nil {
		return "", err
	}

	if device, err = testtools.MakeTestPartition(path, "vfat", 8); err != nil {
		return "", err
	}

	log.Debugf("Create test partition: %s", device)

	if err = syscall.Mount(device, "tmp/mount", "vfat", 0, ""); err != nil {
		return "", err
	}
	defer syscall.Unmount("tmp/mount", 0)

	if err = os.MkdirAll("tmp/mount/EFI/BOOT/NUANCE", 0755); err != nil {
		return "", err
	}

	if output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "create").CombinedOutput(); err != nil {
		return "", fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	syscall.Sync()

	return device, err
}

func deleteBootPartition(device string) (err error) {
	syscall.Sync()

	return testtools.DeleteTestPartition(device)
}

func readEnvVariables(device string) (vars map[string]string, err error) {
	vars = make(map[string]string)

	if err = syscall.Mount(device, "tmp/mount", "vfat", 0, ""); err != nil {
		return nil, err
	}
	defer syscall.Unmount("tmp/mount", 0)

	output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("can't read grubenv: %s, %s", output, err)
	}

	for _, item := range strings.Fields(string(output)) {
		index := strings.Index(item, "=")
		if index < 0 {
			continue
		}

		vars[item[:index]] = item[index+1:]
	}

	return vars, nil
}

func setEnvVariables(device string, vars map[string]string) (err error) {
	if err = syscall.Mount(device, "tmp/mount", "vfat", 0, ""); err != nil {
		return err
	}
	defer syscall.Unmount("tmp/mount", 0)

	for name, value := range vars {
		output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "set", name+"="+value).CombinedOutput()
		if err != nil {
			return fmt.Errorf("can't set grubenv: %s, %s", output, err)
		}
	}

	syscall.Sync()

	return nil
}
