// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dualpartmodule_test

import (
	"aos_updatemanager/updatemodules/partitions/modules/dualpartmodule"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/utils/testtools"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	part0 = iota
	part1
)

const versionFile = "/etc/version.txt"

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStateController struct {
	bootCurrent int
	bootMain    int
	bootOK      bool
}

type testStateStorage struct {
	state []byte
}

type fsContent struct {
	name    string
	content []byte
}

type testChecker struct {
	err error
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var tmpDir string

var disk *testtools.TestDisk

var (
	stateController = testStateController{}
	stateStorage    = testStateStorage{state: []byte("{}")}
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	if disk, err = testtools.NewTestDisk(
		path.Join(tmpDir, "testdisk.img"),
		[]testtools.PartDesc{
			{Type: "ext4", Label: "platform", Size: 32},
			{Type: "ext4", Label: "platform", Size: 32},
		}); err != nil {
		log.Fatalf("Can't create test disk: %s", err)
	}

	if err = createVersionFile(disk.Partitions[part0].Device, "v1.0"); err != nil {
		log.Errorf("Can't create version file: %s", err)
	}

	if err = createVersionFile(disk.Partitions[part1].Device, "v1.0"); err != nil {
		log.Errorf("Can't create version file: %s", err)
	}

	ret := m.Run()

	if err = disk.Close(); err != nil {
		log.Errorf("Can't close test disk: %s", err)
	}

	if err = os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestUpdate(t *testing.T) {
	module, err := dualpartmodule.New("test", []string{
		disk.Partitions[part0].Device,
		disk.Partitions[part1].Device,
	}, versionFile, &stateController, &stateStorage, nil, nil)
	if err != nil {
		t.Fatalf("Can't create test module: %s", err)
	}
	defer module.Close()

	imagePath := path.Join(tmpDir, "image.gz")

	updateVersion := "v2.0"

	imageContent, err := generateImage(imagePath, updateVersion)
	if err != nil {
		t.Fatalf("Can't generate image: %s", err)
	}

	stateController.bootMain = part0
	stateController.bootCurrent = part0
	stateController.bootOK = false

	id := module.GetID()

	if id != "test" {
		t.Errorf("Wrong module id: %s", id)
	}

	// Init

	if err = module.Init(); err != nil {
		t.Fatalf("Error init module: %s", err)
	}

	if !stateController.bootOK {
		t.Error("Error, set boot OK failed")
	}

	// Prepare

	if err = module.Prepare(imagePath, updateVersion, nil); err != nil {
		t.Fatalf("Error prepare module: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Errorf("Error reboot module: %s", err)
	}

	stateController.bootCurrent = part1

	if err = module.Init(); err != nil {
		log.Fatalf("Error init module: %s", err)
	}

	version, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	if version != updateVersion {
		t.Errorf("Wrong vendor version: %s", version)
	}

	// Update

	if rebootRequired, err = module.Update(); err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Apply

	if rebootRequired, err = module.Apply(); err != nil {
		t.Errorf("Error apply module: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Check

	if version, err = module.GetVendorVersion(); err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	if version != updateVersion {
		t.Errorf("Wrong vendor version: %s", version)
	}

	if err = testtools.ComparePartitions(disk.Partitions[part0].Device, disk.Partitions[part1].Device); err != nil {
		t.Errorf("Compare partition error: %s", err)
	}

	updateContent, err := getPartitionContent(disk.Partitions[part1].Device)
	if err != nil {
		t.Errorf("Can't get partition content: %s", err)
	}

	if err = compareContent(imageContent, updateContent); err != nil {
		t.Errorf("Partition content error: %s", err)
	}
}

func TestRevert(t *testing.T) {
	module, err := dualpartmodule.New("test", []string{
		disk.Partitions[part0].Device,
		disk.Partitions[part1].Device,
	}, versionFile, &stateController, &stateStorage, nil, nil)
	if err != nil {
		t.Fatalf("Can't create test module: %s", err)
	}
	defer module.Close()

	initialContent, err := getPartitionContent(disk.Partitions[part0].Device)
	if err != nil {
		t.Errorf("Can't get partition content: %s", err)
	}

	updateVersion := "v3.0"

	imagePath := path.Join(tmpDir, "image.gz")

	if _, err = generateImage(imagePath, updateVersion); err != nil {
		t.Fatalf("Can't generate image: %s", err)
	}

	stateController.bootMain = part0
	stateController.bootCurrent = part0
	stateController.bootOK = false

	// Init

	if err = module.Init(); err != nil {
		t.Fatalf("Error init module: %s", err)
	}

	initialVersion, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	// Prepare

	if err = module.Prepare(imagePath, updateVersion, nil); err != nil {
		t.Fatalf("Error prepare module: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Errorf("Error reboot module: %s", err)
	}

	stateController.bootCurrent = part1

	if err = module.Init(); err != nil {
		log.Fatalf("Error init module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Revert

	if rebootRequired, err = module.Revert(); err != nil {
		t.Errorf("Error revert module: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Errorf("Error reboot module: %s", err)
	}

	stateController.bootCurrent = part0

	if err = module.Init(); err != nil {
		log.Fatalf("Error init module: %s", err)
	}

	// Revert

	if rebootRequired, err = module.Revert(); err != nil {
		t.Errorf("Error revert module: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Check

	version, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	if version != initialVersion {
		t.Errorf("Wrong vendor version: %s", version)
	}

	if err = testtools.ComparePartitions(disk.Partitions[part0].Device, disk.Partitions[part1].Device); err != nil {
		t.Errorf("Compare partition error: %s", err)
	}

	updateContent, err := getPartitionContent(disk.Partitions[part0].Device)
	if err != nil {
		t.Errorf("Partition content error: %s", err)
	}

	if err = compareContent(initialContent, updateContent); err != nil {
		t.Errorf("Partition content error: %s", err)
	}
}

func TestRevertOnFail(t *testing.T) {
	module, err := dualpartmodule.New("test", []string{
		disk.Partitions[part0].Device,
		disk.Partitions[part1].Device,
	}, versionFile, &stateController, &stateStorage, nil, nil)
	if err != nil {
		t.Fatalf("Can't create test module: %s", err)
	}
	defer module.Close()

	initialContent, err := getPartitionContent(disk.Partitions[part0].Device)
	if err != nil {
		t.Errorf("Can't get partition content: %s", err)
	}

	updateVersion := "v3.0"

	imagePath := path.Join(tmpDir, "image.gz")

	if _, err = generateImage(imagePath, updateVersion); err != nil {
		t.Fatalf("Can't generate image: %s", err)
	}

	stateController.bootCurrent = part0
	stateController.bootOK = false

	// Init

	if err = module.Init(); err != nil {
		t.Fatalf("Error init module: %s", err)
	}

	initialVersion, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	// Prepare

	if err = module.Prepare(imagePath, updateVersion, nil); err != nil {
		t.Fatalf("Error prepare module: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Errorf("Error reboot module: %s", err)
	}

	stateController.bootCurrent = part0

	if err = module.Init(); err != nil {
		log.Fatalf("Error init module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err == nil {
		t.Error("Update should fail")
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Revert

	if rebootRequired, err = module.Revert(); err != nil {
		t.Errorf("Error revert module: %s", err)
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}

	// Check

	version, err := module.GetVendorVersion()
	if err != nil {
		t.Errorf("Can't get vendor version: %s", err)
	}

	if version != initialVersion {
		t.Errorf("Wrong vendor version: %s", version)
	}

	if err = testtools.ComparePartitions(disk.Partitions[part0].Device, disk.Partitions[part1].Device); err != nil {
		t.Errorf("Compare partition error: %s", err)
	}

	updateContent, err := getPartitionContent(disk.Partitions[part0].Device)
	if err != nil {
		t.Errorf("Partition content error: %s", err)
	}

	if err = compareContent(initialContent, updateContent); err != nil {
		t.Errorf("Partition content error: %s", err)
	}
}

func TestUpdateChecker(t *testing.T) {
	updateChecker := newTestChecker(nil)

	module, err := dualpartmodule.New("test", []string{
		disk.Partitions[part0].Device,
		disk.Partitions[part1].Device,
	}, versionFile, &stateController, &stateStorage, nil, updateChecker)
	if err != nil {
		t.Fatalf("Can't create test module: %s", err)
	}
	defer module.Close()

	updateVersion := "v3.0"
	imagePath := path.Join(tmpDir, "image.gz")

	if _, err = generateImage(imagePath, updateVersion); err != nil {
		t.Fatalf("Can't generate image: %s", err)
	}

	stateController.bootCurrent = part0
	stateController.bootOK = false

	// Init

	if err = module.Init(); err != nil {
		t.Fatalf("Error init module: %s", err)
	}

	// Prepare

	if err = module.Prepare(imagePath, updateVersion, nil); err != nil {
		t.Fatalf("Error prepare module: %s", err)
	}

	// Update

	rebootRequired, err := module.Update()
	if err != nil {
		t.Errorf("Error update module: %s", err)
	}

	if !rebootRequired {
		t.Errorf("Reboot is required")
	}

	// Reboot

	if err = module.Reboot(); err != nil {
		t.Errorf("Error reboot module: %s", err)
	}

	stateController.bootCurrent = part1
	updateChecker.err = aoserrors.New("update error")

	if err = module.Init(); err != nil {
		log.Fatalf("Error init module: %s", err)
	}

	// Update

	if rebootRequired, err = module.Update(); err == nil {
		t.Error("Update should fail")
	}

	if rebootRequired {
		t.Errorf("Reboot is not required")
	}
}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

// State controller.
func (controller *testStateController) GetCurrentBoot() (index int, err error) {
	return controller.bootCurrent, nil
}

func (controller *testStateController) GetMainBoot() (index int, err error) {
	return controller.bootMain, nil
}

func (controller *testStateController) SetMainBoot(index int) (err error) {
	controller.bootMain = index

	return nil
}

func (controller *testStateController) SetBootOK() (err error) {
	controller.bootOK = true
	return nil
}

func (controller *testStateController) Close() {
}

// State storage.
func (storage *testStateStorage) GetModuleState(id string) (state []byte, err error) {
	return storage.state, nil
}

func (storage *testStateStorage) SetModuleState(id string, state []byte) (err error) {
	storage.state = state

	return nil
}

// Update checker.
func newTestChecker(err error) (checker *testChecker) {
	return &testChecker{err: err}
}

func (checker *testChecker) Check() (err error) {
	return checker.err
}

func generateImage(imagePath string, vendorVersion string) (content []fsContent, err error) {
	if err = os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	content = []fsContent{
		{"/file1.txt", []byte("This is test file 1")},
		{"/file2.txt", []byte("This is test file 2")},
		{"/dir1/file1.txt", []byte("This is test file 1/1")},
		{"/dir1/file2.txt", []byte("This is test file 1/2")},
		{"/dir2/file1.txt", []byte("This is test file 2/1")},
		{"/dir2/file2.txt", []byte("This is test file 2/2")},
		{versionFile, []byte(fmt.Sprintf(`VERSION="%s"`, vendorVersion))},
	}

	if err = testtools.CreateFilePartition(imagePath, "ext4", disk.Partitions[part0].Size,
		func(mountPoint string) (err error) {
			return generateContent(mountPoint, content)
		}, true); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if output, err := exec.Command("mv", imagePath+".gz", imagePath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return content, nil
}

func generateContent(contentPath string, content []fsContent) (err error) {
	for _, file := range content {
		filePath := path.Join(contentPath, file.name)

		if err = os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return aoserrors.Wrap(err)
		}

		if err = ioutil.WriteFile(filePath, file.content, 0o600); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

func getPartitionContent(device string) (content []fsContent, err error) {
	mountPoint, err := ioutil.TempDir(tmpDir, "mount_")
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	defer os.RemoveAll(mountPoint)

	if output, err := exec.Command("mount", device, mountPoint).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	defer func() {
		if output, err := exec.Command("umount", mountPoint).CombinedOutput(); err != nil {
			log.Errorf("Can't unmount folder %s: %s", mountPoint, fmt.Errorf("%s (%s)", err, (string(output))))
		}
	}()

	content = make([]fsContent, 0)

	if err = filepath.Walk(mountPoint, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return aoserrors.Wrap(err)
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(mountPoint, filePath)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		file := fsContent{name: "/" + relPath}

		if file.content, err = ioutil.ReadFile(filePath); err != nil {
			return aoserrors.Wrap(err)
		}

		content = append(content, file)

		return nil
	}); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return content, nil
}

func compareContent(srcContent, dstContent []fsContent) (err error) {
	sort.Slice(srcContent, func(i, j int) bool { return srcContent[i].name < srcContent[j].name })
	sort.Slice(dstContent, func(i, j int) bool { return dstContent[i].name < dstContent[j].name })

	if !reflect.DeepEqual(srcContent, dstContent) {
		return aoserrors.New("content mismatch")
	}

	return nil
}

func createVersionFile(device string, version string) (err error) {
	mountPoint, err := ioutil.TempDir(tmpDir, "mount_")
	if err != nil {
		return aoserrors.Wrap(err)
	}

	defer os.RemoveAll(mountPoint)

	if output, err := exec.Command("mount", device, mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	defer func() {
		if output, err := exec.Command("umount", mountPoint).CombinedOutput(); err != nil {
			log.Errorf("Can't unmount folder %s: %s", mountPoint, fmt.Errorf("%s (%s)", err, (string(output))))
		}
	}()

	filePath := path.Join(mountPoint, versionFile)

	if err = os.MkdirAll(path.Dir(filePath), 0o755); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = ioutil.WriteFile(filePath, []byte(fmt.Sprintf(`VERSION="%s"`, version)), 0o600); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
