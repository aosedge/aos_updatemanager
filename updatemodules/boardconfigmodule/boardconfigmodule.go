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

package boardconfigmodule

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"

	"github.com/aoscloud/aos_common/aoserrors"
	log "github.com/sirupsen/logrus"

	"github.com/aoscloud/aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const ioBufferSize = 1024 * 1024

const (
	newPostfix      = ".new"
	originalPostfix = ".orig"
)

const (
	stateIdle = iota
	statePrepared
	stateUpdated
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// BoardCfgModule board configuration update module.
type BoardCfgModule struct {
	id             string
	currentVersion string
	config         moduleConfig
	storage        updatehandler.ModuleStorage
	state          moduleState
	rebooter       Rebooter
}

// Rebooter provides API to perform module reboot.
type Rebooter interface {
	Reboot() (err error)
}

type boardConfigVersion struct {
	VendorVersion string `json:"vendorVersion"`
}

type moduleConfig struct {
	Path string `json:"path"`
}

type moduleState struct {
	State          updateState `json:"state"`
	RebootRequired bool        `json:"rebootRequired"`
}

type updateState int

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates boardconfig module instance.
func New(id string, configJSON json.RawMessage,
	storage updatehandler.ModuleStorage, rebooter Rebooter) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Debug("Create boardconfig module")

	boardModule := &BoardCfgModule{id: id, storage: storage, rebooter: rebooter}

	if err = json.Unmarshal(configJSON, &boardModule.config); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return boardModule, nil
}

// Close closes boardconfig module.
func (module *BoardCfgModule) Close() (err error) {
	log.WithField("id", module.id).Debug("Close boardconfig module")
	return nil
}

// Init initializes module.
func (module *BoardCfgModule) Init() (err error) {
	if err = module.getState(); err != nil {
		return aoserrors.Wrap(err)
	}

	if module.state.RebootRequired {
		module.state.RebootRequired = false
	}

	if module.currentVersion, err = module.getVendorVersionFromFile(module.config.Path); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// GetID returns module ID.
func (module *BoardCfgModule) GetID() (id string) {
	return module.id
}

// GetVendorVersion returns vendor version.
func (module *BoardCfgModule) GetVendorVersion() (version string, err error) {
	return module.currentVersion, nil
}

// Prepare prepares module.
func (module *BoardCfgModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	log.WithFields(log.Fields{"id": module.id, "fileName": imagePath}).Debug("Prepare")

	switch {
	case module.state.State == statePrepared:
		return nil

	case module.state.State != stateIdle:
		return aoserrors.Errorf("invalid state: %s", module.state.State)
	}

	newBoardConfig := module.config.Path + newPostfix

	if err := extractFileFromGz(newBoardConfig, imagePath); err != nil {
		return aoserrors.Wrap(err)
	}

	newVersion, err := module.getVendorVersionFromFile(newBoardConfig)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if newVersion != vendorVersion {
		os.RemoveAll(newBoardConfig)

		return aoserrors.New("vendor version missmatch")
	}

	if err = module.setState(statePrepared); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// Update updates module.
func (module *BoardCfgModule) Update() (rebootRequired bool, err error) {
	switch {
	case module.state.State == stateUpdated:
		return module.state.RebootRequired, nil

	case module.state.State != statePrepared:
		return false, aoserrors.Errorf("invalid state: %s", module.state.State)
	}

	log.WithFields(log.Fields{"id": module.id}).Debug("Update")

	// save original file
	if err := os.Rename(module.config.Path, module.config.Path+originalPostfix); err != nil {
		log.Warn("Original file does not exist: ", aoserrors.Wrap(err))
	}

	// copy new to original
	if err := os.Rename(module.config.Path+newPostfix, module.config.Path); err != nil {
		return false, aoserrors.Wrap(err)
	}

	module.state.RebootRequired = true

	if err = module.setState(stateUpdated); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return module.state.RebootRequired, nil
}

// Apply applies update.
func (module *BoardCfgModule) Apply() (rebootRequired bool, err error) {
	switch {
	case module.state.State == stateIdle:
		return module.state.RebootRequired, nil

	case module.state.State != stateUpdated:
		return false, aoserrors.Errorf("invalid state: %s", module.state.State)
	}

	log.WithFields(log.Fields{"id": module.id}).Debug("Apply")

	if err = os.RemoveAll(module.config.Path + newPostfix); err != nil {
		log.Errorf("Can't remove file: %s", module.config.Path+newPostfix)
	}

	if err = os.RemoveAll(module.config.Path + originalPostfix); err != nil {
		log.Errorf("Can't remove file: %s", module.config.Path+originalPostfix)
	}

	if err = module.setState(stateIdle); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if module.currentVersion, err = module.getVendorVersionFromFile(module.config.Path); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return module.state.RebootRequired, nil
}

// Revert reverts update.
func (module *BoardCfgModule) Revert() (rebootRequired bool, err error) {
	switch {
	case module.state.State == stateIdle:
		return module.state.RebootRequired, nil

	case module.state.State == stateUpdated:
		if err := os.Rename(module.config.Path+originalPostfix, module.config.Path); err != nil {
			return false, aoserrors.Wrap(err)
		}
	}

	if err = os.RemoveAll(module.config.Path + newPostfix); err != nil {
		log.Errorf("Can't remove file: %s", module.config.Path+newPostfix)
	}

	if err = os.RemoveAll(module.config.Path + originalPostfix); err != nil {
		log.Errorf("Can't remove file: %s", module.config.Path+originalPostfix)
	}

	if module.state.State == stateUpdated && module.state.RebootRequired {
		module.state.RebootRequired = false
	} else {
		module.state.RebootRequired = true
	}

	if err = module.setState(stateIdle); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if module.currentVersion, err = module.getVendorVersionFromFile(module.config.Path); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return module.state.RebootRequired, nil
}

// Reboot performs module reboot.
func (module *BoardCfgModule) Reboot() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Reboot")

	if module.rebooter != nil {
		return aoserrors.Wrap(module.rebooter.Reboot())
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state updateState) String() string {
	return [...]string{"idle", "prepared", "updated"}[state]
}

func (module *BoardCfgModule) getState() (err error) {
	stateJSON, err := module.storage.GetModuleState(module.id)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if len(stateJSON) == 0 {
		return nil
	}

	if err = json.Unmarshal(stateJSON, &module.state); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (module *BoardCfgModule) setState(state updateState) (err error) {
	log.WithFields(log.Fields{"id": module.id, "state": state}).Debug("State changed")

	module.state.State = state

	stateJSON, err := json.Marshal(module.state)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if err = module.storage.SetModuleState(module.id, stateJSON); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (module *BoardCfgModule) getVendorVersionFromFile(path string) (version string, err error) {
	boardFile := boardConfigVersion{}

	byteValue, err := ioutil.ReadFile(path)
	if err != nil {
		return version, aoserrors.Wrap(err)
	}

	if err = json.Unmarshal(byteValue, &boardFile); err != nil {
		return version, aoserrors.Wrap(err)
	}

	return boardFile.VendorVersion, nil
}

func extractFileFromGz(destination, source string) (err error) {
	log.WithFields(log.Fields{"src": source, "dst": destination}).Debug("Extract file from archive")

	srcFile, err := os.Open(source)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE, 0o755)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer gz.Close()

	err = copyData(dstFile, gz)

	return aoserrors.Wrap(err)
}

func copyData(dst io.Writer, src io.Reader) (err error) {
	buf := make([]byte, ioBufferSize)

	for !errors.Is(err, io.EOF) {
		var readCount int

		if readCount, err = src.Read(buf); err != nil && !errors.Is(err, io.EOF) {
			return aoserrors.Wrap(err)
		}

		if readCount > 0 {
			if _, err = dst.Write(buf[:readCount]); err != nil {
				return aoserrors.Wrap(err)
			}
		}
	}

	return nil
}
