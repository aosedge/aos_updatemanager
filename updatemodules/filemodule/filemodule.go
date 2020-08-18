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
// limitations under the License.

package filemodule

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"sync"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const metaDataFilename = "metadata.json"

const ioBufferSize = 1024 * 1024

//
// State machine:
//
// idleState            -> Upgrade() (rebootRequired = true)  -> waitForUpgradeReboot
// waitForUpgradeReboot -> Upgrade() (rebootRequired = false) -> waitForFinish
// waitForFinish        -> FinishUpgrade()                    -> idleState
//

const (
	idleState = iota
	waitForUpgradeReboot
	waitForFinish
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// FileUpdateConfig fs module config
type FileUpdateConfig struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// FileUpdateModuleMetadata upgrade metadata
type FileUpdateModuleMetadata struct {
	ComponentType string `json:"componentType"`
	Resources     []struct {
		ID       string `json:"id"`
		Resource string `json:"resource"`
	} `json:"resources"`
}

// FileModule file update module
type FileModule struct {
	id      string
	config  []FileUpdateConfig
	storage Storage
	state   moduleState
	sync.Mutex
}

// Storage storage interface
type Storage interface {
	GetModuleState(id string) (state []byte, err error)
	SetModuleState(id string, state []byte) (err error)
}

type moduleState struct {
	State upgradeState `json:"state"`
}

type upgradeState int

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates file update module instance
func New(id string, configJSON json.RawMessage,
	storage updatehandler.StateStorage) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Info("Create file update module")

	fileModule := &FileModule{id: id, storage: storage}
	if err = json.Unmarshal(configJSON, &fileModule.config); err != nil {
		return nil, err
	}

	if fileModule.getState(); err != nil {
		return nil, err
	}

	return fileModule, nil
}

// Close closes file update module
func (module *FileModule) Close() (err error) {
	log.WithField("id", module.id).Info("Close file update module")
	return nil
}

// Init initializes module
func (module *FileModule) Init() (err error) {
	return nil
}

// GetID returns module ID
func (module *FileModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Upgrade upgrades module
func (module *FileModule) Upgrade(version uint64, updatePath string) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": updatePath}).Info("Upgrade")

	switch module.state.State {
	case idleState:
		return module.startUpgrade(updatePath)

	case waitForUpgradeReboot:
		return false, nil

	case waitForFinish:
		return false, nil

	default:
		return false, errors.New("invalid state")
	}
}

// CancelUpgrade cancels upgrade
func (module *FileModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

// FinishUpgrade finishes upgrade
func (module *FileModule) FinishUpgrade(version uint64) (err error) {
	return module.setState(idleState)
}

// Revert revert module
func (module *FileModule) Revert(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithField("id", module.id).Info("Revert")

	return false, nil
}

// CancelRevert cancels revert
func (module *FileModule) CancelRevert(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

// FinishRevert finishes revert
func (module *FileModule) FinishRevert(version uint64) (err error) {
	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state upgradeState) String() string {
	return [...]string{
		"Idle", "WaitForUpgradeReboot", "WaitForFinish"}[state]
}

func (module *FileModule) getState() (err error) {
	stateJSON, err := module.storage.GetModuleState(module.id)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(stateJSON, &module.state); err != nil {
		return err
	}

	return nil
}

func (module *FileModule) setState(state upgradeState) (err error) {
	log.WithFields(log.Fields{"state": state}).Debugf("%s: state changed", module.id)

	module.state.State = state

	stateJSON, err := json.Marshal(module.state)
	if err != nil {
		return err
	}

	if err = module.storage.SetModuleState(module.id, stateJSON); err != nil {
		return err
	}

	return nil
}

func (module *FileModule) startUpgrade(updatePath string) (rebootRequired bool, err error) {
	jsonMetadata, err := ioutil.ReadFile(path.Join(updatePath, metaDataFilename))
	if err != nil {
		return false, err
	}

	var updateMetadat FileUpdateModuleMetadata
	if err = json.Unmarshal(jsonMetadata, &updateMetadat); err != nil {
		return false, err
	}

	if updateMetadat.ComponentType != module.id {
		return false, fmt.Errorf("wrong componenet type: %s", updateMetadat.ComponentType)
	}

	var finalError error
	var needReboot bool

	for i := range updateMetadat.Resources {
		configFound := false

		for _, configElement := range module.config {
			if updateMetadat.Resources[i].ID != configElement.ID {
				continue
			}

			configFound = true

			if err := updateFileFromGz(configElement.Path, path.Join(updatePath,
				updateMetadat.Resources[i].Resource)); err != nil {
				log.Errorf("Can't update %s error: %s", configElement.Path, err.Error())

				if finalError == nil {
					finalError = err
				}
			} else {
				if updateMetadat.Resources[i].ID == "deviceconfig" {
					needReboot = true
				}
			}
		}

		if !configFound {
			log.Errorf("Config for  id %s does not exist", updateMetadat.Resources[i].ID)
			if finalError == nil {
				finalError = err
			}
		}
	}

	if needReboot {
		if err = module.setState(waitForUpgradeReboot); err != nil {
			return false, err
		}
	}

	return needReboot, finalError
}

func updateFileFromGz(fileToUpdate, resource string) (err error) {
	log.WithFields(log.Fields{"src": resource, "dst": fileToUpdate}).Debug("Update file from archive")

	srcFile, err := os.Open(resource)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(fileToUpdate, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return err
	}
	defer gz.Close()

	err = copyData(dstFile, gz)

	return err
}

func copyData(dst io.Writer, src io.Reader) (err error) {
	buf := make([]byte, ioBufferSize)

	for err != io.EOF {
		var readCount int

		if readCount, err = src.Read(buf); err != nil && err != io.EOF {
			return err
		}

		if readCount > 0 {
			if _, err = dst.Write(buf[:readCount]); err != nil {
				return err
			}
		}
	}

	return nil
}
