package statecontroller_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"aos_updatemanager/statecontroller"
	"aos_updatemanager/utils/testtools"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	partBoot0 = iota
	partBoot1
	partRoot0
	partRoot1
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

type testEfiProvider struct {
	current   uint16
	next      uint16
	bootOrder []uint16
	items     map[uuid.UUID]uint16
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var tmpDir string
var mountPoint string
var grubEnvFile string

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

var efiProvider = testEfiProvider{items: make(map[uuid.UUID]uint16)}

var configJSON = `
{
	"KernelCmdline" : "$cmdline",
	"StateFile" : "$state",
	"BootPartitions" : ["$device", "$device"],
	"RootPartitions" : ["$device", "$device"]
}`

var disk *testtools.TestDisk

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

	if tmpDir, err = ioutil.TempDir("", "aos_test_"); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	mountPoint = path.Join(tmpDir, "mount")
	grubEnvFile = path.Join(mountPoint, "EFI/BOOT/NUANCE/grubenv")

	if err = os.MkdirAll(mountPoint, 0755); err != nil {
		log.Fatalf("Error creating mount dir: %s", err)
	}

	if disk, err = testtools.NewTestDisk(
		path.Join(tmpDir, "testdisk.img"),
		[]testtools.PartDesc{
			testtools.PartDesc{Type: "vfat", Label: "efi", Size: 8},
			testtools.PartDesc{Type: "vfat", Label: "efi", Size: 8},
			testtools.PartDesc{Type: "ext4", Label: "platform", Size: 16},
			testtools.PartDesc{Type: "ext4", Label: "platform", Size: 16},
		}); err != nil {
		log.Fatalf("Can't create test disk: %s", err)
	}

	configJSON = strings.Replace(configJSON, "$cmdline", path.Join(tmpDir, "cmdline"), 1)
	configJSON = strings.Replace(configJSON, "$state", path.Join(tmpDir, "state"), 1)

	efiProvider.items[disk.Partitions[partBoot0].PartUUID] = 0x0012
	efiProvider.items[disk.Partitions[partBoot1].PartUUID] = 0x0014

	for _, part := range disk.Partitions {
		configJSON = strings.Replace(configJSON, "$device", part.Device, 1)
	}

	if err = initBootPartition(partBoot0); err != nil {
		log.Fatalf("Can't init boot partition: %s", err)
	}

	if err = initBootPartition(partBoot1); err != nil {
		log.Fatalf("Can't init boot partition: %s", err)
	}

	ret := m.Run()

	if err = disk.Close(); err != nil {
		log.Fatalf("Can't close test disk: %s", err)
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestCheckUpdateRootfs(t *testing.T) {
	efiProvider.setCurrentBoot(partBoot0)

	if err := createCmdLine(partRoot0, 0, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider)
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

	env, err := readEnvVariables(partBoot0)
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

	if testModule.path != disk.Partitions[partRoot1].Device {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != disk.Partitions[partRoot0].Type {
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

	if env, err = readEnvVariables(partBoot0); err != nil {
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

	if err = setEnvVariables(partBoot0, map[string]string{"NUANCE_TRY_SWITCH": "0"}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	// Reboot

	if err := controller.Close(); err != nil {
		t.Errorf("Error closing state controller: %s", err)
	}

	if err := createCmdLine(partRoot1, 1, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	if controller, err = statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider); err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}

	postpone, err = controller.UpgradeFinished(1, nil, map[string]error{"rootfs": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}
	if postpone {
		t.Errorf("Upgrade should not be postponed")
	}

	if env, err = readEnvVariables(partBoot0); err != nil {
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

	if testModule.path != disk.Partitions[partRoot0].Device {
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
	efiProvider.setCurrentBoot(partBoot0)

	if err := setEnvVariables(partBoot0, map[string]string{
		"NUANCE_IMAGE_VERSION":       "0",
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(partRoot0, 0, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider)
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

	env, err := readEnvVariables(partBoot0)
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

	if env, err = readEnvVariables(partBoot0); err != nil {
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
	efiProvider.setCurrentBoot(partBoot0)

	if err := setEnvVariables(partBoot0, map[string]string{
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(partRoot1, 1, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider)
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

	env, err := readEnvVariables(partBoot0)
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

	// Finish upgrade
	if _, err = controller.UpgradeFinished(3, nil, nil); err != nil {
		t.Errorf("Finish upgrade failed: %s", err)
	}
}

func TestCheckUpdateBootfs(t *testing.T) {
	efiProvider.SetBootOrder([]uint16{efiProvider.getBootID(partBoot0), efiProvider.getBootID(partBoot1)})
	efiProvider.setCurrentBoot(partBoot0)

	if err := setEnvVariables(partBoot0, map[string]string{
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(partRoot0, 0, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider)
	if err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}
	defer func() {
		if err := controller.Close(); err != nil {
			t.Errorf("Error closing state controller: %s", err)
		}
	}()

	// Start upgrade bootfs

	if err = controller.Upgrade(1, map[string]string{"bootloader": "/path/to/upgrade"}); err != nil {
		t.Fatalf("Can't upgrade: %s", err)
	}

	// Check if correct partition is selected for update

	testModule, err := getTestModule("bootloader")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != disk.Partitions[partBoot1].Device {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != disk.Partitions[partBoot1].Type {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

	// Check if updating bootloader is disable

	if efiProvider.isInBootOrder(partBoot1) {
		t.Errorf("Update boot should not be in boot order")
	}

	// Upgrade finish bootfs

	postpone, err := controller.UpgradeFinished(1, nil, map[string]error{"bootloader": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}

	if !postpone {
		t.Fatal("Upgrade should be postponed")
	}

	// Check that new bootloader boot is scheduled

	nextBoot, _ := efiProvider.GetBootNext()
	if nextBoot != efiProvider.getBootID(partBoot1) {
		t.Errorf("Wrong next boot: %d", nextBoot)
	}

	// Reboot

	if err := controller.Close(); err != nil {
		t.Errorf("Error closing state controller: %s", err)
	}

	efiProvider.setCurrentBoot(partBoot1)

	if controller, err = statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider); err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}

	postpone, err = controller.UpgradeFinished(1, nil, map[string]error{"rootfs": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}
	if postpone {
		t.Errorf("Upgrade should not be postponed")
	}

	// Check boot order

	if !efiProvider.isInBootOrder(partBoot0) || !efiProvider.isInBootOrder(partBoot1) {
		t.Errorf("Both bootloader should be in boot order")
	}

	if efiProvider.bootOrderPos(partBoot0) <= efiProvider.bootOrderPos(partBoot1) {
		t.Errorf("Bootloader boot order should be changed")
	}

	// Check that second partition is updated

	if testModule.path != disk.Partitions[partBoot0].Device {
		t.Errorf("Second bootloader partition wasn't updated")
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

func TestCheckUpdateBootfsRootfs(t *testing.T) {
	efiProvider.SetBootOrder([]uint16{efiProvider.getBootID(partBoot0), efiProvider.getBootID(partBoot1)})
	efiProvider.setCurrentBoot(partBoot0)

	if err := setEnvVariables(partBoot0, map[string]string{
		"NUANCE_DEFAULT_BOOT_INDEX":  "0",
		"NUANCE_FALLBACK_BOOT_INDEX": "1",
	}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	if err := createCmdLine(partRoot0, 0, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	controller, err := statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider)
	if err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}
	defer func() {
		if err := controller.Close(); err != nil {
			t.Errorf("Error closing state controller: %s", err)
		}
	}()

	// Start upgrade bootfs, rootfs

	if err = controller.Upgrade(1, map[string]string{
		"bootloader": "/path/to/upgrade",
		"rootfs":     "/path/to/upgrade"}); err != nil {
		t.Fatalf("Can't upgrade: %s", err)
	}

	// Check if correct partition is selected for update

	testModule, err := getTestModule("bootloader")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != disk.Partitions[partBoot1].Device {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != disk.Partitions[partBoot1].Type {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

	testModule, err = getTestModule("rootfs")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != disk.Partitions[partRoot1].Device {
		t.Errorf("Wrong update partition: %s", testModule.path)
	}

	if testModule.fsType != disk.Partitions[partRoot1].Type {
		t.Errorf("Wrong FS type: %s", testModule.fsType)
	}

	// Check if updating bootloader is disable

	if efiProvider.isInBootOrder(partBoot1) {
		t.Errorf("Update boot should not be in boot order")
	}

	// Check if updating partition is disable

	env, err := readEnvVariables(partBoot0)
	if err != nil {
		t.Fatalf("Can't read grub env variables: %s", err)
	}

	if index := getEnvVariable(t, env, "NUANCE_FALLBACK_BOOT_INDEX"); index != "" {
		t.Errorf("Wrong NUANCE_FALLBACK_BOOT_INDEX value: %s", index)
	}

	// Upgrade finish bootfs, rootfs

	postpone, err := controller.UpgradeFinished(1, nil, map[string]error{
		"bootloader": nil,
		"rootfs":     nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}

	if !postpone {
		t.Fatal("Upgrade should be postponed")
	}

	// Check that new bootloader boot is scheduled

	nextBoot, _ := efiProvider.GetBootNext()
	if nextBoot != efiProvider.getBootID(partBoot1) {
		t.Errorf("Wrong next boot: %d", nextBoot)
	}

	if env, err = readEnvVariables(partBoot0); err != nil {
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

	if err = setEnvVariables(partBoot0, map[string]string{"NUANCE_TRY_SWITCH": "0"}); err != nil {
		t.Fatalf("Can't set grub env variables: %s", err)
	}

	// Reboot

	if err := controller.Close(); err != nil {
		t.Errorf("Error closing state controller: %s", err)
	}

	efiProvider.setCurrentBoot(partBoot1)

	if err := createCmdLine(partRoot1, 1, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	if controller, err = statecontroller.New([]byte(configJSON), &moduleMgr, &efiProvider); err != nil {
		t.Fatalf("Error creating state controller: %s", err)
	}

	postpone, err = controller.UpgradeFinished(1, nil, map[string]error{"rootfs": nil})
	if err != nil {
		t.Fatalf("Can't finish upgrade: %s", err)
	}
	if postpone {
		t.Errorf("Upgrade should not be postponed")
	}

	if env, err = readEnvVariables(partBoot1); err != nil {
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

	testModule, err = getTestModule("rootfs")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != disk.Partitions[partRoot0].Device {
		t.Errorf("Second root FS partition wasn't updated")
	}

	testModule, err = getTestModule("bootloader")
	if err != nil {
		t.Fatalf("Can't get test module: %s", err)
	}

	if testModule.path != disk.Partitions[partBoot0].Device {
		t.Errorf("Second root FS partition wasn't updated")
	}

	// Check boot order

	if !efiProvider.isInBootOrder(partBoot0) || !efiProvider.isInBootOrder(partBoot1) {
		t.Errorf("Both bootloader should be in boot order")
	}

	if efiProvider.bootOrderPos(partBoot0) <= efiProvider.bootOrderPos(partBoot1) {
		t.Errorf("Bootloader boot order should be changed")
	}

	// Check that second partition is updated

	if testModule.path != disk.Partitions[partBoot0].Device {
		t.Errorf("Second bootloader partition wasn't updated")
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

func (provider *testEfiProvider) GetBootByPartUUID(partUUID uuid.UUID) (id uint16, err error) {
	ok := false

	if id, ok = provider.items[partUUID]; !ok {
		return 0, errors.New("item not found")
	}

	return id, nil
}

func (provider *testEfiProvider) GetBootCurrent() (id uint16, err error) {
	return provider.current, nil
}

func (provider *testEfiProvider) GetBootNext() (id uint16, err error) {
	return provider.next, nil
}

func (provider *testEfiProvider) SetBootNext(id uint16) (err error) {
	provider.next = id

	return nil
}

func (provider *testEfiProvider) DeleteBootNext() (err error) {
	return nil
}

func (provider *testEfiProvider) GetBootOrder() (ids []uint16, err error) {
	return provider.bootOrder, nil
}

func (provider *testEfiProvider) SetBootOrder(ids []uint16) (err error) {
	provider.bootOrder = ids

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (provider *testEfiProvider) setCurrentBoot(index int) {
	boot, ok := provider.items[disk.Partitions[index].PartUUID]
	if !ok {
		log.Fatal("Can't set current boot")
	}

	provider.current = boot
}

func (provider *testEfiProvider) getBootID(index int) (id uint16) {
	ok := false

	if id, ok = provider.items[disk.Partitions[index].PartUUID]; !ok {
		log.Fatal("Can't get boot ID")
	}

	return id
}

func (provider *testEfiProvider) isInBootOrder(index int) (result bool) {
	for _, boot := range provider.bootOrder {
		if boot == provider.getBootID(index) {
			return true
		}
	}

	return false
}

func (provider *testEfiProvider) bootOrderPos(index int) (pos int) {
	for i, boot := range provider.bootOrder {
		if boot == provider.getBootID(index) {
			return i
		}
	}

	return -1
}

func createCmdLine(rootDeviceIndex int, bootIndex int, version uint64) (err error) {
	if err = ioutil.WriteFile(path.Join(tmpDir, "cmdline"),
		[]byte(fmt.Sprintf("root=%s NUANCE.bootIndex=%d NUANCE.version=%d",
			disk.Partitions[rootDeviceIndex].Device,
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

func initBootPartition(index int) (err error) {
	if err = syscall.Mount(disk.Partitions[index].Device, mountPoint, disk.Partitions[index].Type, 0, ""); err != nil {
		return err
	}
	defer func() {
		if umountErr := umount(mountPoint); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	if err = os.MkdirAll(filepath.Dir(grubEnvFile), 0755); err != nil {
		return err
	}

	if output, err := exec.Command("grub-editenv", grubEnvFile, "create").CombinedOutput(); err != nil {
		return fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	if output, err := exec.Command("grub-editenv", grubEnvFile, "set",
		"NUANCE_GRUB_CFG_VERSION=1",
		"NUANCE_IMAGE_VERSION=0",
		"NUANCE_DEFAULT_BOOT_INDEX=0",
		"NUANCE_FALLBACK_BOOT_INDEX=1").CombinedOutput(); err != nil {
		return fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	syscall.Sync()

	return err
}

func readEnvVariables(index int) (vars map[string]string, err error) {
	vars = make(map[string]string)

	if err = syscall.Mount(disk.Partitions[index].Device, mountPoint, disk.Partitions[index].Type, unix.MS_RDONLY, ""); err != nil {
		return nil, err
	}
	defer func() {
		if umountErr := umount(mountPoint); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	output, err := exec.Command("grub-editenv", grubEnvFile, "list").CombinedOutput()
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

func setEnvVariables(index int, vars map[string]string) (err error) {
	if err = syscall.Mount(disk.Partitions[index].Device, mountPoint, disk.Partitions[index].Type, 0, ""); err != nil {
		return err
	}
	defer func() {
		if umountErr := umount(mountPoint); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	for name, value := range vars {
		output, err := exec.Command("grub-editenv", grubEnvFile, "set", name+"="+value).CombinedOutput()
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
