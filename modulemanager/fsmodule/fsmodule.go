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

package fsmodule

import (
	"aos_updatemanager/utils/partition"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// FileSystemModule file system update module
type FileSystemModule struct {
	id string
	sync.Mutex
	partitionForUpdate partitionInfo
}

type partitionInfo struct {
	device string
	fsType string
}

type fsUpdateMetadata struct {
	ComponentType string `json:"componentType"`
	Version       int    `json:"version"`
	Description   string `json:"description,omitempty"`
	Type          string `json:"type"`
	Commit        string `json:"commit,omitempty"`
	Resources     string `json:"resources"`
}

/*******************************************************************************
 * Constants
 ******************************************************************************/

const metaDataFilename = "metadata.json"
const tmpMountpoint = "/tmp/aos/mountpoint"
const (
	ostreeRepoFolder = ".ostree_repo"
	ostreeBranchName = "nuance_ota"
)
const (
	incrementalType = "incremental"
	fullType        = "full"
)
const umountRetryCount = 3

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates file system update module instance
func New(id string, configJSON []byte) (module *FileSystemModule, err error) {
	log.WithField("id", id).Info("Create file system update module")

	fsModule := &FileSystemModule{id: id}

	return fsModule, nil
}

// Close closes file system update module
func (module *FileSystemModule) Close() (err error) {
	log.WithField("id", module.id).Info("Close file system update module")
	return nil
}

// GetID returns module ID
func (module *FileSystemModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Upgrade upgrade module
func (module *FileSystemModule) Upgrade(folderPath string) (err error) {
	log.Info("FileSystemModule Upgrade request : ", folderPath)
	jsonFile, err := os.Open(path.Join(folderPath, metaDataFilename))
	if err != nil {
		return err
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)

	var fsMetadata fsUpdateMetadata
	if err = json.Unmarshal(byteValue, &fsMetadata); err != nil {
		return err
	}
	if err = fsMetadata.Validate(folderPath, module.id); err != nil {
		return err
	}

	fsMetadata.Resources = path.Join(folderPath, fsMetadata.Resources)
	if fsMetadata.Type == fullType {
		err = module.performFullFsUpdate(&fsMetadata)
	} else {
		err = module.performIncrementalFsUpdate(&fsMetadata)
	}

	return err
}

// Revert revert module
func (module *FileSystemModule) Revert() (err error) {

	return nil
}

// SetPartitionForUpdate Set partition which should be updated
func (module *FileSystemModule) SetPartitionForUpdate(path, fsType string) (err error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("partition %s does not exist ", path)
	}

	module.partitionForUpdate = partitionInfo{path, fsType}

	return nil
}

// Full fs update
func (module *FileSystemModule) performFullFsUpdate(metadata *fsUpdateMetadata) (err error) {
	log.Debug("Start full file system update, destination: ", module.partitionForUpdate.device)
	partition, err := os.OpenFile(module.partitionForUpdate.device, os.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer partition.Close()

	content, err := os.Open(metadata.Resources)
	if err != nil {
		return err
	}
	defer content.Close()

	gz, err := gzip.NewReader(content)
	if err != nil {
		return err
	}
	defer gz.Close()

	partitionSize, err := partition.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	log.Info("Start full fs Update partition ", module.partitionForUpdate.device, " from ", metadata.Resources)

	var stat syscall.Statfs_t

	syscall.Statfs(module.partitionForUpdate.device, &stat)

	blockSize := stat.Bsize
	buf := make([]byte, blockSize, blockSize)
	total := uint64(0)
	tmpTotal := uint64(0)

	for {
		read, err := gz.Read(buf)
		if err != nil && err != io.EOF {
			log.Error("Could not read contents to pass to partition: ", err)
			return err
		}

		var written int
		if read > 0 {
			tmpTotal = uint64(read) + tmpTotal
			if tmpTotal > uint64(partitionSize) {
				return fmt.Errorf("requested to write at least %d bytes to partition but maximum size is %d", tmpTotal, partitionSize)
			}
			written, err = partition.WriteAt(buf[:read], int64(total))
			if err != nil {
				return err
			}
			// increment our total
			total = total + uint64(written)
		}
		// is this the end of the data?
		if err == io.EOF {
			log.Info("Write partition done, written: ", total)
			break
		}
	}

	return nil
}

// Validate fsUpdateMetadata validation function
func (metadata *fsUpdateMetadata) Validate(resourcePath, id string) (err error) {
	if metadata.ComponentType != id {
		return fmt.Errorf("module type mismatch %s!=%s", metadata.ComponentType, id)
	}

	if metadata.Type != fullType && metadata.Type != incrementalType {
		return fmt.Errorf("unknown fs type update")
	}

	if metadata.Type == incrementalType {
		if metadata.Commit == "" {
			return fmt.Errorf("no commit field for incremental update")
		}
	}

	if _, err := os.Stat(path.Join(resourcePath, metadata.Resources)); os.IsNotExist(err) {
		return fmt.Errorf("resource does not exist %s", path.Join(resourcePath, metadata.Resources))
	}

	return nil
}

func (module *FileSystemModule) performIncrementalFsUpdate(metadata *fsUpdateMetadata) (err error) {
	log.Debug("Start incremental update dev=", module.partitionForUpdate.device, " sources=", metadata.Resources)

	if err = partition.Mount(module.partitionForUpdate.device,
		tmpMountpoint, module.partitionForUpdate.fsType); err != nil {
		return err
	}

	defer func() {
		if umountErr := partition.Umount(tmpMountpoint); umountErr != nil {
			if err == nil {
				err = umountErr
			}
		}
	}()

	repoPath := path.Join(tmpMountpoint, ostreeRepoFolder)
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return fmt.Errorf("ostree repo %s doesnot exist: ", repoPath)
	}

	log.Debug("Apply static delta ")
	if output, err := exec.Command("ostree", "--repo="+repoPath, "static-delta", "apply-offline",
		metadata.Resources).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error %s code: %v", string(output), err)
	}

	log.Debug("Cleanup hard links ")
	if err := removeRepoContents(tmpMountpoint); err != nil {
		return err
	}

	log.Debug("Ostree checkout to commit ", metadata.Commit)
	if output, err := exec.Command("ostree", "--repo="+repoPath, "checkout", metadata.Commit,
		"-H", "-U", "--union", tmpMountpoint).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error %s code: %v", string(output), err)
	}

	log.Debug("Cleanup ostree repo")
	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--delete",
		ostreeBranchName).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error %s code: %v", string(output), err)
	}

	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--create="+ostreeBranchName,
		metadata.Commit).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error %s code: %v", string(output), err)
	}

	if output, err := exec.Command("ostree", "--repo="+repoPath, "prune", ostreeBranchName, "--refs-only",
		"--depth=0").CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error %s code: %v", string(output), err)
	}

	return nil
}

func removeRepoContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == ostreeRepoFolder {
			continue
		}

		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}

	}
	return nil
}
