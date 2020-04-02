package statecontroller_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"aos_updatemanager/statecontroller"
	"aos_updatemanager/utils/efi"
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

const (
	bootID0 = 0x0012
	bootID1 = 0x0014
)

const (
	envGrubConfigVersion = "NUANCE_GRUB_CFG_VERSION"
	envGrubBootOrder     = "NUANCE_BOOT_ORDER"
	envGrubActivePrefix  = "NUANCE_ACTIVE_"
	envGrubBootOKPrefix  = "NUANCE_BOOT_OK_"
	envGrubBootNext      = "NUANCE_BOOT_NEXT"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testBootItem struct {
	id       uint16
	active   bool
	partUUID uuid.UUID
}

type testEfiProvider struct {
	bootCurrent uint16
	bootNext    uint16
	bootOrder   []uint16
	bootItems   []testBootItem
}

type testStorage struct {
	version uint64
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var tmpDir string
var mountPoint string
var grubEnvFile string

var efiProvider = testEfiProvider{
	bootOrder: make([]uint16, 0),
	bootItems: make([]testBootItem, 0),
}
var disk *testtools.TestDisk
var storage testStorage
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

	if tmpDir, err = ioutil.TempDir("", "um_"); err != nil {
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

	efiProvider.bootItems = []testBootItem{
		{bootID0, true, disk.Partitions[partBoot0].PartUUID},
		{bootID1, true, disk.Partitions[partBoot1].PartUUID},
		{0x0011, false, uuid.UUID{}},
	}

	if err = createCmdLine(disk.Partitions[partRoot0].Device, 0); err != nil {
		log.Fatalf("Can't create cmdline file: %s", err)
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

func TestEfiGetCurrentBoot(t *testing.T) {
	type testItem struct {
		bootCurrentID    uint16
		bootCurrentIndex int
		errorExpected    bool
	}

	testData := []testItem{
		{bootID0, 0, false},
		{bootID1, 1, false},
		{0x0011, 0, false},
	}

	for i, item := range testData {
		efiProvider.bootCurrent = item.bootCurrentID

		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)

		switch {
		case item.errorExpected && err != nil:
			continue

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

			continue

		case err != nil:
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		efiController := controller.GetEfiController()

		if err := efiController.WaitForReady(); err != nil {
			t.Fatalf("Wait for ready error: %s", err)
		}

		index, err := efiController.GetCurrentBoot()

		switch {
		case err != nil:
			t.Errorf("Item: %d, can't get current boot: %s", i, err)

		case index != item.bootCurrentIndex:
			t.Errorf("Item: %d, wrong current boot index: %d", i, index)
		}

		controller.Close()
	}
}

func TestEfiGetSetActiveBoot(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	controller, err := statecontroller.New(
		[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
		[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
		&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
	if err != nil {
		t.Fatalf("Can't create test controller: %s", err)
	}
	defer controller.Close()

	efiController := controller.GetEfiController()

	if err := efiController.WaitForReady(); err != nil {
		t.Fatalf("Wait for ready error: %s", err)
	}

	type testItem struct {
		index         int
		active        bool
		errorExpected bool
	}

	testData := []testItem{
		{0, true, false},
		{0, false, false},
		{1, true, false},
		{1, false, false},
		{2, true, true},
	}

	// Get active state

	for i, item := range testData {
		efiProvider.bootItems[item.index].active = item.active

		active, err := efiController.GetBootActive(item.index)

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case !item.errorExpected && err != nil:
			t.Errorf("Item: %d, can't get boot active: %s", i, err)

		case item.active != active:
			t.Errorf("Item: %d, wrong boot active: %v", i, active)
		}
	}

	// Set active state

	for i, item := range testData {
		err := efiController.SetBootActive(item.index, item.active)

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set boot active: %s", i, err)

		case efiProvider.bootItems[item.index].active != item.active:
			t.Errorf("Item: %d, wrong boot active: %v", i, efiProvider.bootItems[item.index].active)
		}
	}
}

func TestEfiGetBootOrder(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	controller, err := statecontroller.New(
		[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
		[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
		&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
	if err != nil {
		t.Fatalf("Can't create test controller: %s", err)
	}
	defer controller.Close()

	type testItem struct {
		bootOrder     []uint16
		bootIndexes   []int
		errorExpected bool
	}

	testData := []testItem{
		{
			[]uint16{0x0000, 0x0002, 0x0003, bootID0, bootID1, 0x0004},
			[]int{0, 1},
			false,
		},
		{
			[]uint16{0x0000, bootID1, 0x0002, 0x0003, bootID0, 0x0004},
			[]int{1, 0},
			false,
		},
		{
			[]uint16{0x0000, 0x0002, 0x0003, bootID0, 0x0004},
			nil,
			true,
		},
	}

	efiController := controller.GetEfiController()

	if err := efiController.WaitForReady(); err != nil {
		t.Fatalf("Wait for ready error: %s", err)
	}

	for i, item := range testData {
		efiProvider.bootOrder = item.bootOrder

		bootOrder, err := efiController.GetBootOrder()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't get boot order: %s", i, err)

		case !reflect.DeepEqual(bootOrder, item.bootIndexes):
			log.Errorf("Wrong boot order: %v", bootOrder)
		}
	}
}

func TestEfiSetBootOrder(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	controller, err := statecontroller.New(
		[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
		[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
		&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
	if err != nil {
		t.Fatalf("Can't create test controller: %s", err)
	}
	defer controller.Close()

	type testItem struct {
		initBootOrder   []uint16
		resultBootOrder []uint16
		bootIndexes     []int
		errorExpected   bool
	}

	testData := []testItem{
		{
			[]uint16{0x0000, 0x0002, 0x0003, bootID0, bootID1, 0x0004},
			[]uint16{bootID0, bootID1, 0x0000, 0x0002, 0x0003, 0x0004},
			[]int{0, 1},
			false,
		},
		{
			[]uint16{0x0000, bootID1, 0x0002, 0x0003, bootID0, 0x0004},
			[]uint16{bootID1, bootID0, 0x0000, 0x0002, 0x0003, 0x0004},
			[]int{1, 0},
			false,
		},
		{
			[]uint16{0x0000, 0x0002, 0x0003, bootID0, 0x0004},
			[]uint16{bootID1, bootID0, 0x0000, 0x0002, 0x0003, 0x0004},
			[]int{1, 0},
			false,
		},
		{
			[]uint16{0x0000, 0x0002, 0x0003, bootID0, 0x0004},
			nil,
			[]int{3, 5, 3},
			true,
		},
	}

	efiController := controller.GetEfiController()

	if err := efiController.WaitForReady(); err != nil {
		t.Fatalf("Wait for ready error: %s", err)
	}

	for i, item := range testData {
		efiProvider.bootOrder = item.initBootOrder

		err := efiController.SetBootOrder(item.bootIndexes)

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set boot order: %s", i, err)

		case !reflect.DeepEqual(efiProvider.bootOrder, item.resultBootOrder):
			log.Errorf("Item: %d, wrong boot order: %v, %v", i, efiProvider.bootOrder, item.resultBootOrder)
		}
	}
}

func TestEfiSetClearBootNext(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	controller, err := statecontroller.New(
		[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
		[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
		&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
	if err != nil {
		t.Fatalf("Can't create test controller: %s", err)
	}
	defer controller.Close()

	type testItem struct {
		nextIndex     int
		nextBoot      uint16
		errorExpected bool
	}

	testData := []testItem{
		{0, bootID0, false},
		{1, bootID1, false},
		{2, 0x0000, true},
	}

	efiController := controller.GetEfiController()

	if err := efiController.WaitForReady(); err != nil {
		t.Fatalf("Wait for ready error: %s", err)
	}

	for i, item := range testData {
		err := efiController.SetBootNext(item.nextIndex)

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set next boot: %s", i, err)

		case efiProvider.bootNext != item.nextBoot:
			t.Errorf("Item: %d, wrong next boot: 0x%04X", i, efiProvider.bootNext)
		}
	}

	if err := efiController.ClearBootNext(); err != nil {
		t.Errorf("Can't clear next boot: %s", err)
	}

	if efiProvider.bootNext != 0xFFFF {
		t.Errorf("Wrong next boot: 0x%04X", efiProvider.bootNext)
	}
}

func TestGRUBGetCurrentBoot(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	type testItem struct {
		rootPart         string
		bootCurrentIndex int
		errorExpected    bool
	}

	testData := []testItem{
		{disk.Partitions[partRoot0].Device, 0, false},
		{disk.Partitions[partRoot1].Device, 1, false},
		{disk.Partitions[partRoot0].Device, 1, true},
		{disk.Partitions[partRoot0].Device, 2, true},
		{"/dev/unknownPart", 1, true},
	}

	for i, item := range testData {
		if err := createCmdLine(item.rootPart, item.bootCurrentIndex); err != nil {
			t.Fatalf("Can't create cmdline file: %s", err)
		}

		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)

		switch {
		case item.errorExpected && err != nil:
			continue

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

			continue

		case err != nil:
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		index, err := grubController.GetCurrentBoot()

		switch {
		case err != nil:
			t.Errorf("Item: %d, can't get current boot: %s", i, err)

		case index != item.bootCurrentIndex:
			t.Errorf("Item: %d, wrong current boot index: %d", i, index)
		}

		controller.Close()
	}
}

func TestGRUBGetSetActiveBoot(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	if err := createCmdLine(disk.Partitions[partRoot0].Device, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	type testItem struct {
		index         int
		active        bool
		errorExpected bool
	}

	testData := []testItem{
		{0, true, false},
		{0, false, false},
		{1, true, false},
		{1, false, false},
		{2, true, true},
	}

	// Get active state

	for i, item := range testData {
		value := "0"
		if item.active {
			value = "1"
		}

		if err := setEnvVariables(partBoot0,
			map[string]string{envGrubActivePrefix + strconv.Itoa(item.index): value}); err != nil {
			t.Fatalf("Can't set env variables: %s", err)
		}

		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
		if err != nil {
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		active, err := grubController.GetBootActive(item.index)

		controller.Close()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't get boot active: %s", i, err)

		case item.active != active:
			t.Errorf("Item: %d, wrong boot active: %v", i, active)
		}
	}

	// Set active state

	for i, item := range testData {
		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
		if err != nil {
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		err = grubController.SetBootActive(item.index, item.active)

		controller.Close()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set boot active: %s", i, err)

		default:
			envVars, err := getEnvVariables(partBoot0)
			if err != nil {
				t.Fatalf("Item: %d, can't read env variables: %s", i, err)
			}

			varName := envGrubActivePrefix + strconv.Itoa(item.index)

			valueStr, ok := envVars[varName]
			if !ok {
				t.Errorf("Item: %d, env variable %s not found", i, varName)
				break
			}

			value, err := strconv.Atoi(valueStr)
			if err != nil {
				t.Errorf("Item: %d, can't convert value: %s", i, err)
				break
			}

			active := false

			if value != 0 {
				active = true
			}

			if item.active != active {
				t.Errorf("Item: %d, wrong boot active: %v", i, active)
			}
		}
	}
}

func TestGRUBGetSetBootOrder(t *testing.T) {
	efiProvider.bootCurrent = bootID0

	if err := createCmdLine(disk.Partitions[partRoot0].Device, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	type testItem struct {
		setBootOrder  []int
		envBootOrder  string
		errorExpected bool
	}

	testData := []testItem{
		{[]int{0, 1}, "0 1", false},
		{[]int{1, 0}, "1 0", false},
		{[]int{2, 0}, "2 0", true},
	}

	// Get boot order

	for i, item := range testData {
		if err := setEnvVariables(partBoot0,
			map[string]string{envGrubBootOrder: item.envBootOrder}); err != nil {
			t.Fatalf("Can't set env variables: %s", err)
		}

		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
		if err != nil {
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		bootOrder, err := grubController.GetBootOrder()

		controller.Close()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't get boot order: %s", i, err)

		case !reflect.DeepEqual(item.setBootOrder, bootOrder):
			t.Errorf("Item: %d, wrong boot order: %v", i, bootOrder)
		}
	}

	// Set boot order

	for i, item := range testData {
		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
		if err != nil {
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		err = grubController.SetBootOrder(item.setBootOrder)

		controller.Close()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set boot order: %s", i, err)

		default:
			envVars, err := getEnvVariables(partBoot0)
			if err != nil {
				t.Fatalf("Item: %d, can't read env variables: %s", i, err)
			}

			valueStr, ok := envVars[envGrubBootOrder]
			if !ok {
				t.Errorf("Item: %d, env variable %s not found", i, envGrubBootOrder)
				break
			}

			if valueStr != item.envBootOrder {
				t.Errorf("Item: %d, wrong boot order: %s", i, valueStr)
			}
		}
	}
}

func TestGRUBSetClearBootNext(t *testing.T) {
	efiProvider.bootCurrent = bootID0
	efiProvider.bootNext = 0xFFFF

	if err := createCmdLine(disk.Partitions[partRoot0].Device, 0); err != nil {
		t.Fatalf("Can't create cmdline file: %s", err)
	}

	type testItem struct {
		efiBootNext   int
		efiBootPart   int
		grubBootNext  int
		errorExpected bool
	}

	testData := []testItem{
		{-1, partBoot0, 0, false},
		{-1, partBoot0, 1, false},
		{-1, partBoot0, 2, true},
		{1, partBoot1, 0, false},
		{1, partBoot1, 1, false},
		{1, partBoot1, 2, true},
	}

	// Set boot next

	for i, item := range testData {
		controller, err := statecontroller.New(
			[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
			[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
			&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
		if err != nil {
			t.Fatalf("Item: %d, can't create state controller: %s", i, err)
		}

		grubController := controller.GetGrubController()

		if err := grubController.WaitForReady(); err != nil {
			t.Fatalf("Item: %d, wait for ready error: %s", i, err)
		}

		if item.efiBootNext >= 0 {
			if err = controller.GetEfiController().SetBootNext(item.efiBootNext); err != nil {
				t.Fatalf("Item: %d, can't set efi boot next: %s", i, err)
			}
		}

		err = grubController.SetBootNext(item.grubBootNext)

		controller.Close()

		switch {
		case item.errorExpected && err != nil:

		case item.errorExpected && err == nil:
			t.Errorf("Item: %d, error expected", i)

		case err != nil:
			t.Errorf("Item: %d, can't set boot next: %s", i, err)

		default:
			envVars, err := getEnvVariables(item.efiBootPart)
			if err != nil {
				t.Fatalf("Item: %d, can't read env variables: %s", i, err)
			}

			valueStr, ok := envVars[envGrubBootNext]
			if !ok {
				t.Errorf("Item: %d, env variable %s not found", i, envGrubBootNext)
				break
			}

			value, err := strconv.Atoi(valueStr)

			if value != item.grubBootNext {
				t.Errorf("Item: %d, wrong boot next: %d", i, value)
			}
		}
	}

	// Clear boot next
	controller, err := statecontroller.New(
		[]string{disk.Partitions[partBoot0].Device, disk.Partitions[partBoot1].Device},
		[]string{disk.Partitions[partRoot0].Device, disk.Partitions[partRoot1].Device},
		&storage, path.Join(tmpDir, "cmdline"), &efiProvider)
	if err != nil {
		t.Fatalf("Can't create state controller: %s", err)
	}

	grubController := controller.GetGrubController()

	if err := grubController.WaitForReady(); err != nil {
		t.Fatalf("Wait for ready error: %s", err)
	}

	if err := grubController.SetBootNext(1); err != nil {
		t.Fatalf("Can't set boot next: %s", err)
	}

	if err := grubController.ClearBootNext(); err != nil {
		t.Fatalf("Can't clear boot next: %s", err)
	}

	if err := controller.Close(); err != nil {
		t.Fatalf("Can't close GRUB controller: %s", err)
	}

	envVars, err := getEnvVariables(partBoot0)
	if err != nil {
		t.Fatalf("Can't read env variables: %s", err)
	}

	if _, ok := envVars[envGrubBootNext]; ok {
		t.Error("Boot next var is not expected")
	}
}

/*******************************************************************************
 * Interface
 ******************************************************************************/

// Storage

func (storage *testStorage) GetSystemVersion() (version uint64, err error) {
	return storage.version, nil
}

func (storage *testStorage) SetSystemVersion(version uint64) (err error) {
	storage.version = version

	return nil
}

// EFI provider

func (provider *testEfiProvider) GetBootByPartUUID(partUUID uuid.UUID) (id uint16, err error) {
	for _, item := range provider.bootItems {
		if item.partUUID == partUUID {
			return item.id, nil
		}
	}

	return 0, efi.ErrNotFound
}

func (provider *testEfiProvider) GetBootCurrent() (id uint16, err error) {
	return provider.bootCurrent, nil
}

func (provider *testEfiProvider) GetBootNext() (id uint16, err error) {
	if provider.bootNext == 0xFFFF {
		return 0, efi.ErrNotFound
	}

	return provider.bootNext, nil
}

func (provider *testEfiProvider) SetBootNext(id uint16) (err error) {
	provider.bootNext = id

	return nil
}

func (provider *testEfiProvider) DeleteBootNext() (err error) {
	provider.bootNext = 0xFFFF

	return nil
}

func (provider *testEfiProvider) GetBootOrder() (ids []uint16, err error) {
	return provider.bootOrder, nil
}

func (provider *testEfiProvider) SetBootOrder(ids []uint16) (err error) {
	provider.bootOrder = ids

	return nil
}

func (provider *testEfiProvider) GetBootActive(id uint16) (active bool, err error) {
	for _, item := range provider.bootItems {
		if item.id == id {
			return item.active, nil
		}
	}

	return false, efi.ErrNotFound
}

func (provider *testEfiProvider) SetBootActive(id uint16, active bool) (err error) {
	for i, item := range provider.bootItems {
		if item.id == id {
			provider.bootItems[i].active = active

			return nil
		}
	}

	return efi.ErrNotFound
}

func (provider *testEfiProvider) Close() (err error) {
	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func createCmdLine(rootPart string, bootIndex int) (err error) {
	if err = ioutil.WriteFile(path.Join(tmpDir, "cmdline"),
		[]byte(fmt.Sprintf("root=%s NUANCE.bootIndex=%d", rootPart, bootIndex)), 0644); err != nil {
		return err
	}

	return nil
}

func getEnvVariables(index int) (vars map[string]string, err error) {
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

	for _, item := range strings.Split(string(output), "\n") {
		index := strings.Index(item, "=")
		if index < 0 {
			continue
		}

		vars[item[:index]] = item[index+1:]
	}

	return vars, nil
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
		"NUANCE_GRUB_CFG_VERSION=2",
		"NUANCE_IMAGE_VERSION=0",
		"NUANCE_DEFAULT_BOOT_INDEX=0",
		"NUANCE_FALLBACK_BOOT_INDEX=1").CombinedOutput(); err != nil {
		return fmt.Errorf("can't create grubenv: %s, %s", output, err)
	}

	syscall.Sync()

	return err
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
