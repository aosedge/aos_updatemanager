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
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

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
			"device" : "/dev/root",
			"fstype" : "ext4"
		},
		{
			"device" : "/dev/root",
			"fstype" : "ext4"
		}
	]
}`

var bootParts = []string{"", ""}
var rootParts = []string{"", ""}

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
		// Create boot partition
		device, err := createBootPartition("tmp/boot" + strconv.Itoa(i))
		if err != nil {
			log.Fatalf("Can't create boot partition: %s", err)
		}

		configJSON = strings.Replace(configJSON, "/dev/boot", device, 1)

		bootParts[i] = device

		// Create root partition
		if device, err = createRootPartition("tmp/root" + strconv.Itoa(i)); err != nil {
			log.Fatalf("Can't create root partition: %s", err)
		}

		configJSON = strings.Replace(configJSON, "/dev/root", device, 1)

		rootParts[i] = device
	}

	ret := m.Run()

	// Delete boot partitions
	for _, bootPart := range bootParts {
		if err := deleteBootPartition(bootPart); err != nil {
			log.Errorf("Can't delete test boot part: %s", err)
		}
	}

	// Delete root partitions
	for _, rootPart := range rootParts {
		if err := deleteRootPartition(rootPart); err != nil {
			log.Errorf("Can't delete test root part: %s", err)
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
	if err := createCmdLine(bootParts[0], rootParts[0], 0, 0); err != nil {
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

	env, err := readEnvVariables(bootParts[0])
	if err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check if updating partition is disable

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Check if correct partition is selected for update

	testModule, err := getTestModule("rootfs")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != rootParts[1] {
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

	if env, err = readEnvVariables(bootParts[0]); err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check boot OK

	if bootOK := getEnvVariable(t, env, "NUANCE_BOOT_OK"); bootOK != "1" {
		t.Errorf("Wrong NUANCE_BOOT_OK value: %s", bootOK)
	}

	// Check that fallback partition is set

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "1" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Check that rootfs switch scheduled

	if trySwitch := getEnvVariable(t, env, "NUANCE_TRY_SWITCH"); trySwitch != "1" {
		t.Errorf("Wrong NUANCE_TRY_SWITCH value: %s", trySwitch)
	}

	if err = setEnvVariables(bootParts[0], map[string]string{"NUANCE_TRY_SWITCH": "0"}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	// Reboot

	if err := controller.Close(); err != nil {
		t.Errorf("Error closing state controller: %s", err)
	}

	if err := createCmdLine(bootParts[0], rootParts[1], 1, 0); err != nil {
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

	// Check env var version

	if version := getEnvVariable(t, env, "NUANCE_IMAGE_VERSION"); version != "1" {
		t.Errorf("Wrong NUANCE_IMAGE_VERSION value: %s", version)
	}

	// Check env default and fallback boot indexes

	if index := getEnvVariable(t, env, "NUANCE_DEFAULT_BOOT_INDEX"); index != "1" {
		t.Errorf("Wrong NUANCE_DEFAULT_BOOT_INDEX value: %s", index)
	}

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "0" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Check that second partition is updated

	if testModule.path != rootParts[0] {
		t.Errorf("Second root FS partition wasn't updated")
	}

	// Check if version is updated
	version, err := controller.GetVersion()
	if err != nil {
		t.Fatalf("Can't get controller version: %s", err)
	}

	if version != 1 {
		t.Errorf("Wrong controller version: %d", version)
	}
}

func TestCheckUpdateRootfsFail(t *testing.T) {
	if err := setEnvVariables(bootParts[0], map[string]string{
		"NUANCE_IMAGE_VERSION":       "0",
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(bootParts[0], rootParts[0], 0, 0); err != nil {
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

	env, err := readEnvVariables(bootParts[0])
	if err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check if updating partition is disable

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Notify SC that rootfs update failed

	postpone, err := controller.UpgradeFinished(1, errors.New("failed"), map[string]error{"rootfs": errors.New("failed")})
	if err == nil {
		t.Errorf("Upgrade should fail")
	}

	if postpone {
		t.Errorf("Upgrade should not be postponed")
	}

	if env, err = readEnvVariables(bootParts[0]); err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check env var version

	if version := getEnvVariable(t, env, "NUANCE_IMAGE_VERSION"); version != "0" {
		t.Errorf("Wrong NUANCE_IMAGE_VERSION value: %s", version)
	}

	// Check env default and fallback boot indexes

	if index := getEnvVariable(t, env, "NUANCE_DEFAULT_BOOT_INDEX"); index != "0" {
		t.Errorf("Wrong NUANCE_DEFAULT_BOOT_INDEX value: %s", index)
	}

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "1" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Check if version is updated
	version, err := controller.GetVersion()
	if err != nil {
		t.Fatalf("Can't get controller version: %s", err)
	}

	if version != 0 {
		t.Errorf("Wrong controller version: %d", version)
	}
}

func TestBootFallbackPartition(t *testing.T) {
	if err := setEnvVariables(bootParts[0], map[string]string{
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(bootParts[0], rootParts[1], 1, 0); err != nil {
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

	// Call upgrade to wait system check finish
	if err = controller.Upgrade(3, nil); err != nil {
		t.Errorf("Upgrade failed: %s", err)
	}

	env, err := readEnvVariables(bootParts[0])
	if err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	// Check default and fallback boot indexes

	if index := getEnvVariable(t, env, "NUANCE_DEFAULT_BOOT_INDEX"); index != "1" {
		t.Errorf("Wrong NUANCE_DEFAULT_BOOT_INDEX value: %s", index)
	}

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "0" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
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

	log.Debugf("Create boot partition: %s", device)

	if err = syscall.Mount(device, "tmp/mount", "vfat", 0, ""); err != nil {
		return "", err
	}
	defer func() {
		if umountErr := umount("tmp/mount"); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	if err = os.MkdirAll("tmp/mount/EFI/BOOT/NUANCE", 0755); err != nil {
		return "", err
	}

	if output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "create").CombinedOutput(); err != nil {
		return "", fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	if output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "set",
		"NUANCE_GRUB_CFG_VERSION=1",
		"NUANCE_IMAGE_VERSION=0",
		"NUANCE_DEFAULT_BOOT_INDEX=0",
		"NUANCE_FALLBACK_BOOT_INDEX=1").CombinedOutput(); err != nil {
		return "", fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	syscall.Sync()

	return device, err
}

func createRootPartition(path string) (device string, err error) {
	if err = os.MkdirAll("tmp/mount", 0755); err != nil {
		return "", err
	}

	if device, err = testtools.MakeTestPartition(path, "ext4", 16); err != nil {
		return "", err
	}

	log.Debugf("Create test root partition: %s", device)

	if err = syscall.Mount(device, "tmp/mount", "ext4", 0, ""); err != nil {
		return "", err
	}
	defer func() {
		if umountErr := umount("tmp/mount"); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	syscall.Sync()

	return device, err
}

func deleteBootPartition(device string) (err error) {
	syscall.Sync()

	log.Debugf("Delete test boot partition: %s", device)

	return testtools.DeleteTestPartition(device)
}

func deleteRootPartition(device string) (err error) {
	syscall.Sync()

	log.Debugf("Delete test root partition: %s", device)

	return testtools.DeleteTestPartition(device)
}

func readEnvVariables(device string) (vars map[string]string, err error) {
	vars = make(map[string]string)

	if err = syscall.Mount(device, "tmp/mount", "vfat", unix.MS_RDONLY, ""); err != nil {
		return nil, err
	}
	defer func() {
		if umountErr := umount("tmp/mount"); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

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
	defer func() {
		if umountErr := umount("tmp/mount"); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	for name, value := range vars {
		output, err := exec.Command("grub-editenv", "tmp/mount/EFI/BOOT/NUANCE/grubenv", "set", name+"="+value).CombinedOutput()
		if err != nil {
			return fmt.Errorf("can't set grubenv: %s, %s", output, err)
		}
	}

	syscall.Sync()

	return nil
}

func umount(mountPoint string) (err error) {
	for i := 0; i < 3; i++ {
		syscall.Sync()

		if err = syscall.Unmount(mountPoint, 0); err == nil {
			return
		}

		time.Sleep(1 * time.Second)
	}

	log.Errorf("Can't umount %s: %s", mountPoint, err)

	return err
}

func getEnvVariable(t *testing.T, vars map[string]string, name string) (value string) {
	var ok bool

	if value, ok = vars[name]; !ok {
		t.Errorf("variable %s not found", name)
	}

	return value
}
