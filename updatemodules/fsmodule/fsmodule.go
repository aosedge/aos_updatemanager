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
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// FileSystemModule file system update module
type FileSystemModule struct {
	id string
	sync.Mutex
	partitionForUpdate string
}

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

	return err
}

// Revert revert module
func (module *FileSystemModule) Revert() (err error) {

	return nil
}

// SetPartitionForUpdate Set partition which should be updated
func (module *FileSystemModule) SetPartitionForUpdate(path string) (err error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("partition %s does not exist ", path)
	}
	module.partitionForUpdate = path
	return nil
}
