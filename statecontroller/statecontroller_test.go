package statecontroller_test

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

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
