// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2019 Renesas Inc.
// Copyright 2019 EPAM Systems Inc.
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
// limitations under the License

package fsmodule_test

import (
	fsmodule "aos_updatemanager/modulemanager/fsmodule"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type fsMetadata struct {
	ComponentType string `json:"componentType"`
	Version       int    `json:"version"`
	Description   string `json:"description,omitempty"`
	Type          string `json:"type"`
	Commit        string `json:"commit,omitempty"`
	Resources     string `json:"resources"`
}

/*******************************************************************************
 * Var
 ******************************************************************************/

var module *fsmodule.FileSystemModule

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
		log.Fatalf("Error creating tmp dir %s", err)
	}

	module, err = fsmodule.New("rootfs", []byte(""))
	if err != nil {
		log.Fatalf("Can't create Rootfs module: %s", err)
	}

	ret := m.Run()

	module.Close()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error deleting tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id := module.GetID()
	if id != "rootfs" {
		t.Errorf("Wrong module ID: %s", id)
	}
}

func TestSetPartition(t *testing.T) {
	err := module.SetPartitionForUpdate("", "")
	if err == nil {
		t.Errorf("Should be error: partition does not exist")
	}

	err = module.SetPartitionForUpdate("/tmp", "ext4")
	if err != nil {
		t.Errorf("Error SetPartitionForUpdate: %s", err)
	}
}

func TestParamsValidation(t *testing.T) {
	err := module.Upgrade("./NoFolder")
	if err == nil {
		t.Errorf("Should be error file does not exist %s", err)
	}

	if err := ioutil.WriteFile("tmp/metadata.json", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: in json parcing %s", err)
	}

	if err := ioutil.WriteFile("tmp/metadata.json", []byte(`{
			"componentType": "notrootfs",
			"version": 12,
			"description": "Nuance rootfs v 12",
			"type": "incremental",
			"commit" : "5b1c9137cc8fc487b6158b34e7f088c809558e4c",
			"resources" : "folder_path"
		  }`), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: not rootfs update")
	}

	if err := ioutil.WriteFile("tmp/metadata.json", []byte(`{
		"componentType": "rootfs",
		"version": 12,
		"description": "Nuance rootfs v 12",
		"type": "unknown",
		"commit" : "5b1c9137cc8fc487b6158b34e7f088c809558e4c",
		"resources" : "folder_path"
	  }`), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: unknown rootfs update type")
	}

	if err := ioutil.WriteFile("tmp/metadata.json", []byte(`{
		"componentType": "rootfs",
		"version": 12,
		"description": "Nuance rootfs v 12",
		"type": "incremental",		
		"resources" : "folder_path"
	  }`), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: No commit for incremental update")
	}

	if err := ioutil.WriteFile("tmp/metadata.json", []byte(`{
		"componentType": "rootfs",
		"version": 12,
		"description": "Nuance rootfs v 12",
		"type": "full",		
		"resources" : "folder_path"
	  }`), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}
	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: resource does not exist")
	}
}

func TestFullFSUpdate(t *testing.T) {
	if err := ioutil.WriteFile("tmp/metadata.json", []byte(`{
		"componentType": "rootfs",
		"version": 12,
		"description": "Nuance rootfs v 12",
		"type": "full",		
		"resources" : "testImage.img.gz"
	  }`), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := ioutil.WriteFile("tmp/testImage.img.gz", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: resource does not exist %s", err)
	}

	partition := "./tmp/partition"
	generateTestPartition(10, partition)

	if err := module.SetPartitionForUpdate(partition, "ext4"); err != nil {
		t.Errorf("Error SetPartitionForUpdate: %s", err)
	}

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: invalid header %s", err)
	}
	generateTestImage("./tmp/", prepareContentForFullUpdate)

	if err := module.Upgrade("tmp"); err == nil {
		t.Errorf("Upgrade should failed: Size missmatch %s", err)
	}

	generateTestPartition(20, partition)

	if err := module.Upgrade("tmp"); err != nil {
		t.Errorf("Upgrade failed %s", err)
	}

	if err := os.MkdirAll("tmp/mount_point_new", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}

	command := exec.Command("mount", partition, "./tmp/mount_point_new")
	if err := command.Run(); err != nil {
		t.Errorf("Can't mount updated partiion: %s", err)
	}

	if _, err := os.Stat("./tmp/mount_point_new/testdata.txt"); os.IsNotExist(err) {
		t.Errorf("Resource does not exist")
	}

	command = exec.Command("umount", "./tmp/mount_point_new")
	if err := command.Run(); err != nil {
		t.Errorf("Can't umount updated partiion: %s", err)
	}
}

func TestIncrementalUpdate(t *testing.T) {
	if os.Getenv("CI") != "" {
		log.Debug("Skip Incremental Update test due to container setup issue")
		return
	}

	ostreeTestpath := "tmp/ostree/test/"
	if err := os.MkdirAll(ostreeTestpath, 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}

	generateTestImage(ostreeTestpath, prepareResourceForIncUpdate)

	metadata := fsMetadata{
		ComponentType: "rootfs",
		Version:       12,
		Description:   "Nuance rootfs v 12",
		Type:          "full",
		Resources:     "testImage.img.gz",
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		log.Fatalf("Can't marshall json: %s", err)
	}

	if err := ioutil.WriteFile(path.Join(ostreeTestpath, "metadata.json"), data, 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	partition := "tmp/ostree/partition"
	generateTestPartition(20, partition)

	//generate loop
	loopDevice := "/dev/loop97"
	if err := exec.Command("losetup", loopDevice, partition).Run(); err != nil {
		log.Fatalf("Can't run losetup: %s", err)
	}

	if err := module.SetPartitionForUpdate(loopDevice, "ext4"); err != nil {
		t.Errorf("Error SetPartitionForUpdate: %s", err)
	}

	if err := module.Upgrade(ostreeTestpath); err != nil {
		t.Errorf("Upgrade failed %s", err)
	}

	commit := prepareDiffcommit()
	metadata.Commit = commit
	metadata.Type = "incremental"
	metadata.Version = 13
	metadata.Resources = "delta"

	data, err = json.Marshal(metadata)
	if err != nil {
		log.Fatalf("Can't marshall json: %s", err)
	}

	if err := ioutil.WriteFile(path.Join(ostreeTestpath, "metadata.json"), data, 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := module.Upgrade(ostreeTestpath); err != nil {
		t.Errorf("Upgrade failed with delta %s", err)
	}

	if err := exec.Command("losetup", "-d", loopDevice).Run(); err != nil {
		log.Fatalf("Can't remove loop: %s", err)
	}

	//change validation
	tmpMountPint := "./tmp/mount_point"
	if err := os.MkdirAll(tmpMountPint, 0755); err != nil {
		log.Fatalf("Error creating tmp mount_point %s", err)
	}

	if err := exec.Command("mount", partition, tmpMountPint).Run(); err != nil {
		log.Fatalf("Can't run mount for updated partition: %s", err)
	}

	defer func() {
		if err := exec.Command("umount", tmpMountPint).Run(); err != nil {
			log.Fatalf("Can't run umount: %s", err)
		}
	}()

	if _, err := os.Stat(path.Join(tmpMountPint, "dir1/file3.txt")); os.IsNotExist(err) {
		t.Errorf("Resource dir1/file3.txt does not exist")
	}

	if _, err := os.Stat(path.Join(tmpMountPint, "file_for_remove.txt")); os.IsNotExist(err) == false {
		t.Errorf("file_for_remove.txt should not be exist")
	}
}

func generateTestImage(folderForRes string, imageContent func(mountPoint string)) (archivePath string) {
	mountPointImg := "./tmp/mount_point"
	pathToImage := path.Join(folderForRes, "testImage.img")

	generateTestPartition(20, pathToImage)

	if err := exec.Command("mkfs.ext4", pathToImage).Run(); err != nil {
		log.Fatalf("Can't run mkfs.ext4: %s", err)
	}

	if err := os.MkdirAll(mountPointImg, 0755); err != nil {
		log.Fatalf("Error creating tmp mount_point %s", err)
	}

	if err := exec.Command("mount", pathToImage, mountPointImg).Run(); err != nil {
		log.Fatalf("Can't run mount: %s", err)
	}

	imageContent(mountPointImg)

	if err := exec.Command("umount", mountPointImg).Run(); err != nil {
		log.Fatalf("Can't run umount: %s", err)
	}

	if err := os.RemoveAll("./tmp/testImage.img.gz"); err != nil {
		log.Fatalf("Error deleting ./tmp/testImage.img.gz : %s", err)
	}

	if err := exec.Command("gzip", path.Join(folderForRes, "testImage.img")).Run(); err != nil {
		log.Fatalf("Can't run gzip: %s", err)
	}
	if err := os.RemoveAll(mountPointImg); err != nil {
		log.Fatalf("Error deleting %s : %s", mountPointImg, err)
	}

	return path.Join(folderForRes, "testImage.img.gz")
}

func prepareContentForFullUpdate(mountPoint string) {
	if err := ioutil.WriteFile(path.Join(mountPoint, "testdata.txt"), []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}
}

func generateTestPartition(size int, pathPartition string) {
	count := "count=" + strconv.Itoa(size)

	command := exec.Command("dd", "if=/dev/zero", "of="+pathPartition, "bs=1M", count)
	err := command.Run()
	if err != nil {
		log.Fatalf("Generate test partition failed: %s", err)
	}
}

// prepare full image with ostree repo
func prepareResourceForIncUpdate(mountpath string) {
	//prepare source folder and create archive
	if err := os.MkdirAll("tmp/origOstree/", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}

	//ostree --repo=tmp/origOstree/.ostree_repo init --mode=bare-user
	if err := exec.Command("ostree", "--repo=tmp/origOstree/.ostree_repo", "init", "--mode=bare-user").Run(); err != nil {
		log.Fatalf("Error creating repo %s", err)
	}

	if err := os.MkdirAll("tmp/archive_folder", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}
	if err := os.MkdirAll("tmp/archive_folder/dir1", 0755); err != nil {
		log.Fatalf("Error creating tmp dir %s", err)
	}
	if err := ioutil.WriteFile("tmp/archive_folder/file.txt", []byte("This is test file"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}
	if err := ioutil.WriteFile("tmp/archive_folder/file_for_remove.txt", []byte("This is test file to test remove"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}
	if err := ioutil.WriteFile("tmp/archive_folder/dir1/file2.txt", []byte("This is test file2"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := exec.Command("tar", "-czf", "tmp/test_archive.tar.gz", "-C", "tmp/archive_folder", ".").Run(); err != nil {
		log.Fatalf("Can't run tar: %s", err)
	}

	//ostree --repo=tmp/origOstree/.ostree_repo commit -b branch_name -s"commit name" --tree=tar=tmp/test_archive.tar.gz
	commit, err := exec.Command("ostree", "--repo=tmp/origOstree/.ostree_repo", "commit", "-b", "branch_name", "-s", `"commit name"`,
		"--tree=tar=tmp/test_archive.tar.gz").Output()
	if err != nil {
		log.Fatal(err)
	}

	commitstr := strings.TrimSuffix(string(commit), "\n")
	log.Debug("Commit= ", commitstr)

	if err := exec.Command("ostree", "--repo="+path.Join(mountpath, ".ostree_repo"), "init", "--mode=bare-user").Run(); err != nil {
		log.Fatalf("Error creating repo in mountpath %s", err)
	}

	if err := exec.Command("ostree", "--repo="+path.Join(mountpath, ".ostree_repo"), "pull-local", "tmp/origOstree/.ostree_repo").Run(); err != nil {
		log.Fatalf("Error pull-local %s", err)
	}

	if err := exec.Command("ostree", "--repo="+path.Join(mountpath, ".ostree_repo"), "checkout", commitstr, "-H", "-U", "--union", mountpath).Run(); err != nil {
		log.Fatal("Error chcekout ", err)
	}
}

func prepareDiffcommit() (commitId string) {
	if err := ioutil.WriteFile("tmp/archive_folder/dir1/file3.txt", []byte("This is test file2"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}

	if err := ioutil.WriteFile("tmp/archive_folder/dir1/file2.txt", []byte("Edited File"), 0644); err != nil {
		log.Fatalf("Can't write test file: %s", err)
	}
	if err := os.RemoveAll("tmp/archive_folder/file_for_remove.txt"); err != nil {
		log.Fatalf("Can't remove file: %s", err)
	}

	if err := exec.Command("tar", "-czf", "tmp/test_archive2.tar.gz", "-C", "tmp/archive_folder", ".").Run(); err != nil {
		log.Fatalf("Can't run tar: %s", err)
	}

	commit, err := exec.Command("ostree", "--repo=tmp/origOstree/.ostree_repo", "commit", "-b", "branch_name", "-s", `"commit name2"`,
		"--tree=tar=tmp/test_archive2.tar.gz").Output()
	if err != nil {
		log.Fatal("error commit ", err)
	}

	commitstr := strings.TrimSuffix(string(commit), "\n")

	if err := exec.Command("ostree", "--repo=tmp/origOstree/.ostree_repo", "static-delta", "generate", "branch_name",
		"--filename=tmp/ostree/test/delta").Run(); err != nil {
		log.Fatal("error static-delta ", err)
	}

	return commitstr
}
