package fsmodule_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/modules/fsmodule"
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

type testStateController struct {
	bootCurrent int
	bootNext    int
	bootOrder   []int
	bootActive  []bool
}

type testStateStorage struct {
	state []byte
}

type fsContent struct {
	name    string
	content []byte
}

type actionNew struct {
	state testStateController
}

type actionReboot struct {
	state testStateController
}

type actionClose struct {
}

type actionUpgrade struct {
	version        uint64
	imagePath      string
	rebootRequired bool
	state          testStateController
}

type actionCancel struct {
	version        uint64
	rebootRequired bool
	state          testStateController
}

type actionFinish struct {
	version uint64
	state   testStateController
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var tmpDir string

var configJSON = `
{
	"Partitions" : ["$device", "$device"]
}`

var disk *testtools.TestDisk

var stateController = testStateController{}
var stateStorage = testStateStorage{state: []byte("")}

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

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	if disk, err = testtools.NewTestDisk(
		path.Join(tmpDir, "testdisk.img"),
		[]testtools.PartDesc{
			testtools.PartDesc{Type: "vfat", Label: "efi", Size: 16},
			testtools.PartDesc{Type: "vfat", Label: "efi", Size: 16},
			testtools.PartDesc{Type: "ext4", Label: "platform", Size: 32},
			testtools.PartDesc{Type: "ext4", Label: "platform", Size: 32},
		}); err != nil {
		log.Fatalf("Can't create test disk: %s", err)
	}

	configJSON = strings.Replace(configJSON, "$cmdline", path.Join(tmpDir, "cmdline"), 1)

	for part := partRoot0; part <= partRoot1; part++ {
		configJSON = strings.Replace(configJSON, "$device", disk.Partitions[part].Device, 1)
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

func TestGetID(t *testing.T) {
	module, err := fsmodule.New("testfs", &stateController, &stateStorage, []byte(configJSON))
	if err != nil {
		t.Fatalf("Can't create testfs module: %s", err)
	}
	defer module.Close()

	id := module.GetID()
	if id != "testfs" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestParamsValidation(t *testing.T) {
	testMetadataData := []string{
		"",
		"This is test file",
		`{
	"componentType": "unknown",
	"version": 12,
	"description": "Nuance rootfs v 12",
	"type": "incremental",
	"commit" : "5b1c9137cc8fc487b6158b34e7f088c809558e4c",
	"resources" : "image.dat"
 }`,
		`{
	"componentType": "testfs",
	"version": 12,
	"description": "Nuance rootfs v 12",
	"type": "unknown",
	"commit" : "5b1c9137cc8fc487b6158b34e7f088c809558e4c",
	"resources" : "image.dat"
}`,
		`{
	"componentType": "testfs",
	"version": 12,
	"description": "Nuance rootfs v 12",
	"type": "incremental",
	"resources" : "image.dat"
}`,
		`{
	"componentType": "testfs",
	"version": 12,
	"description": "Nuance rootfs v 12",
	"type": "full",
	"resources" : "image.dat"
}`,
		`{
	"componentType": "testfs",
	"version": 12,
	"description": "Nuance rootfs v 12",
	"type": "full",
	"resources" : "notexist.dat"
}`,
	}

	module, _ := doAction(t, nil, actionNew{testStateController{0, -1, []int{0, 1}, []bool{true, true}}}, false)
	defer doAction(t, module, actionClose{}, false)

	for _, metadata := range testMetadataData {
		if metadata != "" {
			if err := ioutil.WriteFile(path.Join(tmpDir, "metadata.json"), []byte(metadata), 0644); err != nil {
				t.Fatalf("Can't write test file: %s", err)
			}
		} else {
			os.RemoveAll(path.Join(tmpDir, "metadata.json"))
		}

		if err := ioutil.WriteFile(path.Join(tmpDir, "image.dat"), []byte("Upgrade image"), 0644); err != nil {
			t.Fatalf("Can't write test file: %s", err)
		}

		_, err := doAction(t, module, actionUpgrade{version: 1, imagePath: tmpDir}, true)
		if err != nil {
			// This log is just to display error for debug purpose
			log.Errorf("Upgrade failed: %s", err)
		}

		if err == nil {
			t.Error("Error expected")
		}
	}
}

func TestUpgrade(t *testing.T) {
	type testItem struct {
		metadata       fsmodule.Metadata
		imageGenerator func(imagePath string) (content []fsContent, commit string, err error)
	}

	testData := []testItem{
		{
			fsmodule.Metadata{
				ComponentType: "testfs",
				Type:          "full",
				Resources:     "test.img.gz",
			},
			generateFullUpgradeImage,
		},
	}

	if os.Getenv("CI") != "" {
		log.Warn("Skip Incremental Update test due to container setup issue")
	} else {
		incrementalTestData := []testItem{
			{
				fsmodule.Metadata{
					ComponentType: "testfs",
					Type:          "full",
					Resources:     "test.img.gz",
				},
				generateInitialIncrementalImage,
			},
			{
				fsmodule.Metadata{
					ComponentType: "testfs",
					Type:          "incremental",
					Resources:     "test.img.gz",
				},
				generateSecondIncrementalImage,
			},
		}

		testData = append(testData, incrementalTestData...)
	}

	for _, item := range testData {
		// Generate image

		imageContent, commit, err := item.imageGenerator(path.Join(tmpDir, item.metadata.Resources))
		if err != nil {
			t.Fatalf("Can't create test image: %s", err)
		}

		item.metadata.Commit = commit

		if err = createMetadata(path.Join(tmpDir, "metadata.json"), item.metadata); err != nil {
			t.Fatalf("Can't create test metadata: %s", err)
		}

		// Create module

		stateController = testStateController{0, -1, []int{0, 1}, []bool{true, true}}

		module, _ := doAction(t, nil, actionNew{state: stateController}, false)

		// First upgrade

		doAction(t, module, actionUpgrade{
			version:        1,
			imagePath:      tmpDir,
			rebootRequired: true,
			state:          testStateController{0, 1, []int{0, 1}, []bool{true, false}}}, false)

		// Reboot

		stateController.bootCurrent = 1
		stateController.bootNext = -1

		module, _ = doAction(t, module, actionReboot{state: stateController}, false)

		// Second upgrade

		doAction(t, module, actionUpgrade{
			version:        1,
			imagePath:      tmpDir,
			rebootRequired: false,
			state:          testStateController{1, -1, []int{1, 0}, []bool{false, true}}}, false)

		// Finish

		doAction(t, module, actionFinish{
			version: 1,
			state:   testStateController{1, -1, []int{1, 0}, []bool{false, true}}}, false)

		// Wait for upgrading second partition by calling cancel upgrade

		doAction(t, module, actionCancel{
			version: 1,
			state:   testStateController{1, -1, []int{1, 0}, []bool{true, true}}}, false)

		// Check content of upgraded partition

		partitionContent, err := getPartitionContent(disk.Partitions[partRoot1].Device)
		if err != nil {
			t.Fatalf("Can't get partition content: %s", err)
		}

		if err = compareContent(imageContent, partitionContent); err != nil {
			t.Errorf("Compare content error: %s", err)
		}

		// Check content of initial partition

		if partitionContent, err = getPartitionContent(disk.Partitions[partRoot0].Device); err != nil {
			t.Fatalf("Can't get partition content: %s", err)
		}

		if err = compareContent(imageContent, partitionContent); err != nil {
			t.Errorf("Compare content error: %s", err)
		}

		doAction(t, module, actionClose{}, false)
	}
}

func TestBadUpgrade(t *testing.T) {
	if err := createMetadata(path.Join(tmpDir, "metadata.json"),
		fsmodule.Metadata{
			ComponentType: "testfs",
			Type:          "full",
			Resources:     "test.img.gz",
		}); err != nil {
		t.Fatalf("Can't create test metadata: %s", err)
	}

	// Test upgrade bigger than upgraded partition

	if err := testtools.CreateFilePartition(path.Join(tmpDir, "test.img"), "ext4",
		disk.Partitions[partRoot1].Size+4, nil, true); err != nil {
		t.Fatalf("Can't create test image: %s", err)
	}

	// Create module

	module, _ := doAction(t, nil, actionNew{testStateController{0, -1, []int{0, 1}, []bool{true, true}}}, false)
	defer doAction(t, module, actionClose{}, false)

	// First upgrade

	if _, err := doAction(t, module, actionUpgrade{version: 1, imagePath: tmpDir}, true); err == nil {
		t.Error("Upgrade should failed due to size limitation")
	}

	// Wait for upgrading second partition by calling cancel upgrade

	doAction(t, module, actionCancel{
		version: 1,
		state:   testStateController{0, -1, []int{0, 1}, []bool{true, true}}}, false)

	if err := testtools.ComparePartitions(disk.Partitions[partRoot0].Device,
		disk.Partitions[partRoot1].Device); err != nil {
		t.Errorf("Comapre partition error: %s", err)
	}
}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

// State controller

func (controller *testStateController) WaitForReady() (err error) {
	return nil
}

func (controller *testStateController) GetCurrentBoot() (index int, err error) {
	return controller.bootCurrent, nil
}

func (controller *testStateController) SetBootNext(index int) (err error) {
	controller.bootNext = index

	return nil
}

func (controller *testStateController) ClearBootNext() (err error) {
	controller.bootNext = -1

	return nil
}

func (controller *testStateController) SetBootActive(index int, active bool) (err error) {
	if index < 0 || index >= len(controller.bootActive) {
		return errors.New("invalid index")
	}

	controller.bootActive[index] = active

	return nil
}

func (controller *testStateController) GetBootActive(index int) (active bool, err error) {
	if index < 0 || index >= len(controller.bootActive) {
		return false, errors.New("invalid index")
	}

	return controller.bootActive[index], nil
}

func (controller *testStateController) GetBootOrder() (bootOrder []int, err error) {
	return controller.bootOrder, nil
}

func (controller *testStateController) SetBootOrder(bootOrder []int) (err error) {
	controller.bootOrder = bootOrder

	return nil
}

// State storage

func (storage *testStateStorage) GetModuleState(id string) (state []byte, err error) {
	return storage.state, nil
}

func (storage *testStateStorage) SetModuleState(id string, state []byte) (err error) {
	storage.state = state

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func doAction(t *testing.T, module *fsmodule.FSModule, action interface{}, errorExpected bool) (newModule *fsmodule.FSModule, err error) {
	t.Helper()

	newModule = module

	switch data := action.(type) {
	case actionNew:
		stateController = data.state

		if newModule, err = fsmodule.New("testfs", &stateController, &stateStorage, []byte(configJSON)); err != nil && !errorExpected {
			t.Fatalf("Can't create testfs module: %s", err)
		}

		if err = newModule.Init(); err != nil && !errorExpected {
			t.Errorf("Can't initialize testfs module: %s", err)
		}

	case actionReboot:
		if err = module.Close(); err != nil && !errorExpected {
			t.Errorf("Close module error: %s", err)
		}

		stateController = data.state

		if newModule, err = fsmodule.New("testfs", &stateController, &stateStorage, []byte(configJSON)); err != nil && !errorExpected {
			t.Fatalf("Can't create testfs module: %s", err)
		}

		if err = newModule.Init(); err != nil && !errorExpected {
			t.Errorf("Can't initialize testfs module: %s", err)
		}

	case actionClose:
		if err = module.Close(); err != nil && !errorExpected {
			t.Errorf("Close module error: %s", err)
		}

	case actionUpgrade:
		rebootRequired := false

		if rebootRequired, err = module.Upgrade(data.version, data.imagePath); err != nil && !errorExpected {
			t.Errorf("Upgrade error: %s", err)
		}

		if rebootRequired != data.rebootRequired && !errorExpected {
			t.Errorf("Wrong reboot required value: %v", rebootRequired)
		}

		if err = stateController.compareState(data.state); err != nil && !errorExpected {
			t.Errorf("Compare state error: %v", stateController)
		}

	case actionCancel:
		rebootRequired := false

		if rebootRequired, err = module.CancelUpgrade(data.version); err != nil && !errorExpected {
			t.Errorf("Cancel upgrade error: %s", err)
		}

		if rebootRequired != data.rebootRequired && !errorExpected {
			t.Errorf("Wrong reboot required value: %v", rebootRequired)
		}

		if err = stateController.compareState(data.state); err != nil && !errorExpected {
			t.Errorf("Compare state error: %v", stateController)
		}

	case actionFinish:
		if err = module.FinishUpgrade(data.version); err != nil && !errorExpected {
			t.Errorf("Finish upgrade error: %s", err)
		}

		if err = stateController.compareState(data.state); err != nil && !errorExpected {
			t.Errorf("Compare state error: %v", stateController)
		}
	}

	return newModule, err
}

func (controller *testStateController) compareState(state testStateController) (err error) {
	if controller.bootCurrent != state.bootCurrent {
		return errors.New("wrong current boot value")
	}

	if controller.bootNext != state.bootNext {
		return errors.New("wrong next boot value")
	}

	if !reflect.DeepEqual(controller.bootOrder, state.bootOrder) {
		return errors.New("wrong boot order value")
	}

	if !reflect.DeepEqual(controller.bootActive, state.bootActive) {
		return errors.New("wrong boot order value")
	}

	return nil
}

func createMetadata(path string, metadata fsmodule.Metadata) (err error) {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(path, metadataJSON, 0644); err != nil {
		return err
	}

	return nil
}

func generateFullUpgradeImage(imagePath string) (content []fsContent, commit string, err error) {
	if err = os.MkdirAll(filepath.Dir(imagePath), 0755); err != nil {
		return nil, "", err
	}

	content = []fsContent{
		{"file1.txt", []byte("This is test file 1")},
		{"file2.txt", []byte("This is test file 2")},
		{"dir1/file1.txt", []byte("This is test file 1/1")},
		{"dir1/file2.txt", []byte("This is test file 1/2")},
		{"dir2/file1.txt", []byte("This is test file 2/1")},
		{"dir2/file2.txt", []byte("This is test file 2/2")},
	}

	if err = testtools.CreateFilePartition(imagePath, "ext4", disk.Partitions[partRoot1].Size,
		func(mountPoint string) (err error) {
			return generateContent(mountPoint, content)
		}, true); err != nil {
		return nil, "", err
	}

	if output, err := exec.Command("mv", imagePath+".gz", imagePath).CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return content, "", nil
}

func generateInitialIncrementalImage(imagePath string) (content []fsContent, commit string, err error) {
	ostreeRepo := path.Join(tmpDir, ".ostree_repo")

	if err = ostreeInit(ostreeRepo); err != nil {
		return nil, "", err
	}

	contentPath := path.Join(tmpDir, "content.tar.gz")

	content = []fsContent{
		{"file.txt", []byte("This is test file")},
		{"file_for_remove.txt", []byte("This is test file to test remove")},
		{"dir1/file2.txt", []byte("This is test file2")},
	}

	if err = generateTarContent(contentPath, content); err != nil {
		return nil, "", err
	}

	if commit, err = ostreeCommit(ostreeRepo, "test_branch", "Initial commit", contentPath); err != nil {
		return nil, "", err
	}

	if testtools.CreateFilePartition(imagePath, "ext4", disk.Partitions[partRoot1].Size,
		func(mountPoint string) (err error) {
			return generateInitialImage(mountPoint, ostreeRepo, commit)
		}, true); err != nil {
		return nil, "", err
	}

	if output, err := exec.Command("mv", imagePath+".gz", imagePath).CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return content, commit, nil
}

func generateSecondIncrementalImage(imagePath string) (content []fsContent, commit string, err error) {
	ostreeRepo := path.Join(tmpDir, ".ostree_repo")

	content = []fsContent{
		{"file.txt", []byte("This is test file")},
		{"dir1/file2.txt", []byte("This is edited test file2")},
	}

	contentPath := path.Join(tmpDir, "content.tar.gz")

	if err = generateTarContent(contentPath, content); err != nil {
		return nil, "", err
	}

	if commit, err = ostreeCommit(ostreeRepo, "test_branch", "Increment commit", contentPath); err != nil {
		return nil, "", err
	}

	if err = ostreeStaticDelta(ostreeRepo, "test_branch", imagePath); err != nil {
		return nil, "", err
	}

	return content, commit, nil
}

func ostreeInit(repoPath string) (err error) {
	if err = os.RemoveAll(repoPath); err != nil {
		return err
	}

	if err = os.MkdirAll(repoPath, 0755); err != nil {
		return err
	}

	//ostree --repo=tmp/origOstree/.ostree_repo init --mode=bare-user
	if output, err := exec.Command("ostree", "--repo="+repoPath, "init", "--mode=bare-user").CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return nil
}

func ostreeCommit(repoPath, branch, subject, contentPath string) (commit string, err error) {
	var output []byte

	if output, err = exec.Command("ostree", "--repo="+repoPath, "commit", "-b", branch, "-s", subject,
		"--tree=tar="+contentPath).Output(); err != nil {
		return "", fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}

func ostreeStaticDelta(repoPath, to, deltaFile string) (err error) {
	if output, err := exec.Command("ostree", "--repo="+repoPath, "static-delta", "generate",
		to, "--filename="+deltaFile).CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return nil
}

func generateContent(contentPath string, content []fsContent) (err error) {
	for _, file := range content {
		filePath := path.Join(contentPath, file.name)

		if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}

		if err = ioutil.WriteFile(filePath, file.content, 0644); err != nil {
			return err
		}
	}

	return nil
}

func generateTarContent(filePath string, content []fsContent) (err error) {
	contentDir, err := ioutil.TempDir(tmpDir, "content_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(contentDir)

	if err = generateContent(contentDir, content); err != nil {
		return err
	}

	if output, err := exec.Command("tar", "-czf", filePath, "-C", contentDir, ".").CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return nil
}

func generateInitialImage(mountPoint, ostreeRepo, commit string) (err error) {
	localRepo := path.Join(mountPoint, ".ostree_repo")

	if output, err := exec.Command("ostree", "--repo="+localRepo, "init", "--mode=bare-user").CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err := exec.Command("ostree", "--repo="+localRepo, "pull-local", ostreeRepo).CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err := exec.Command("ostree", "--repo="+localRepo, "checkout", commit, "-H", "-U", "--union", mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("%s (%s)", err, (string(output)))
	}

	return nil
}

func getPartitionContent(device string) (content []fsContent, err error) {
	mountPoint, err := ioutil.TempDir(tmpDir, "mount_")
	if err != nil {
		return nil, err
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
			return err
		}

		if info.IsDir() {
			if info.Name() == ".ostree_repo" {
				return filepath.SkipDir
			}

			return nil
		}

		relPath, err := filepath.Rel(mountPoint, filePath)
		if err != nil {
			return err
		}

		file := fsContent{name: relPath}

		if file.content, err = ioutil.ReadFile(filePath); err != nil {
			return err
		}

		content = append(content, file)

		return nil
	}); err != nil {
		return nil, err
	}

	return content, nil
}

func compareContent(srcContent, dstContent []fsContent) (err error) {
	sort.Slice(srcContent, func(i, j int) bool { return srcContent[i].name < srcContent[j].name })
	sort.Slice(dstContent, func(i, j int) bool { return dstContent[i].name < dstContent[j].name })

	if !reflect.DeepEqual(srcContent, dstContent) {
		return errors.New("content mismatch")
	}

	return nil
}
