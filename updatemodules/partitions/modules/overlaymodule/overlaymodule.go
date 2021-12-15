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

package overlaymodule

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/aoserrors"

	"aos_updatemanager/database"
	"aos_updatemanager/updatehandler"
)

// Success update sequence diagram:
//
// Prepare(path)  -> set state "prepared"
// Update()       -> set requestReboot
// Reboot()       -> requestReboot is set, perform system reboot
//------------------------------- Reboot ---------------------------------------
// Init()         -> boot OK, set state "updated", clear requestReboot
// Update()       -> return OK, already in "updated" state
// Reboot()       -> return OK, requestReboot is not set
// Apply()        -> set requestReboot
// Reboot()       -> requestReboot is set, perform system reboot
//------------------------------- Reboot ---------------------------------------
// Init()         -> if boot OK, set state "idle", clear requestReboot
// Apply()        -> return OK, already in "idle" state
// Reboot()       -> return OK, requestReboot is not set

// Failed update sequence diagram:
//
// Prepare(path)  -> set state "prepared"
// Update()       -> set requestReboot
// Reboot()       -> requestReboot is set, perform system reboot
//------------------------------- Reboot ---------------------------------------
// Init()         -> boot not OK, set update error, clear requestReboot
// Update()       -> return update error
// Revert()       -> set state idle
// Reboot()       -> retrun OK, requestReboot is not set

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	idleState = iota
	preparedState
	updatedState
)

const (
	doUpdateFileName = "do_update"
	doApplyFileName  = "do_apply"
	updatedFileName  = "updated"
	failedFileName   = "failed"
	imageExtension   = ".squashfs"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// OverlayModule overlay module
type OverlayModule struct {
	id             string
	versionFile    string
	updateDir      string
	storage        updatehandler.ModuleStorage
	state          moduleState
	bootWithUpdate bool
	bootErr        error
	rebooter       Rebooter
	checker        UpdateChecker
	vendorVersion  string
}

// Rebooter performs module reboot
type Rebooter interface {
	Reboot() (err error)
}

// UpdateChecker handler for checking update
type UpdateChecker interface {
	Check() (err error)
}

type moduleState struct {
	UpdateState updateState `json:"updateState"`
	UpdateType  string      `json:"updateType"`
}

type updateState int

type moduleMetadata struct {
	Type string `json:"type"`
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates module instance
func New(id string, versionFile, updateDir string,
	storage updatehandler.ModuleStorage, rebooter Rebooter,
	checker UpdateChecker) (module updatehandler.UpdateModule, err error) {
	log.WithFields(log.Fields{"id": id}).Debug("Create overlay module")

	if storage == nil {
		return nil, aoserrors.New("no storage provided")
	}

	overlayModule := &OverlayModule{
		id: id, versionFile: versionFile, updateDir: updateDir, storage: storage,
		rebooter: rebooter, checker: checker}

	if overlayModule.versionFile == "" {
		return nil, aoserrors.New("version file is not set")
	}

	if overlayModule.updateDir == "" {
		return nil, aoserrors.New("update dir is nit set")
	}

	if err = overlayModule.getState(); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return overlayModule, nil
}

// Close closes module
func (module *OverlayModule) Close() (err error) {
	log.WithField("id", module.id).Debug("Close overlay module")

	return nil
}

// Init initializes module
func (module *OverlayModule) Init() (err error) {
	defer func() {
		if err != nil && module.bootErr == nil {
			module.bootErr = aoserrors.Wrap(err)
		}

		if module.bootErr != nil {
			log.WithFields(log.Fields{"id": module.id}).Errorf("Module boot error: %s", module.bootErr)
		}
	}()

	log.WithFields(log.Fields{"id": module.id}).Debug("Init overlay module")

	if module.vendorVersion, err = module.getModuleVersion(); err != nil {
		return aoserrors.Wrap(err)
	}

	if module.state.UpdateState == idleState {
		return nil
	}

	updatedFile := path.Join(module.updateDir, updatedFileName)

	if _, err = os.Stat(updatedFile); err == nil {
		module.bootWithUpdate = true
	}

	failedFile := path.Join(module.updateDir, failedFileName)

	if _, err = os.Stat(failedFile); err == nil {
		if module.bootErr == nil {
			module.bootErr = aoserrors.New("boot failed")
		}

		if err = os.RemoveAll(failedFile); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	if module.checker != nil && module.bootErr == nil {
		module.bootErr = aoserrors.Wrap(module.checker.Check())
	}

	return nil
}

// GetID returns module ID
func (module *OverlayModule) GetID() (id string) {
	return module.id
}

// GetVendorVersion returns vendor version
func (module *OverlayModule) GetVendorVersion() (version string, err error) {
	return module.vendorVersion, nil
}

// Prepare prepares module update
func (module *OverlayModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	log.WithFields(log.Fields{
		"id":            module.id,
		"imagePath":     imagePath,
		"vendorVersion": vendorVersion}).Debug("Prepare overlay module")

	if module.state.UpdateState != idleState && module.state.UpdateState != preparedState {
		return aoserrors.Errorf("wrong state: %s", module.state.UpdateState)
	}

	var metadata moduleMetadata

	if err = json.Unmarshal(annotations, &metadata); err != nil {
		return aoserrors.Wrap(err)
	}

	module.state.UpdateType = metadata.Type
	module.state.UpdateState = preparedState

	if err = module.clearUpdateDir(); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = os.Rename(imagePath, path.Join(module.updateDir, path.Base(imagePath)+imageExtension)); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = module.saveState(); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// Update performs module update
func (module *OverlayModule) Update() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Update overlay module")

	if module.state.UpdateState == updatedState {
		if !module.bootWithUpdate {
			return false, aoserrors.New("boot with update failed")
		}

		if module.bootErr != nil {
			return false, aoserrors.Wrap(module.bootErr)
		}

		if err = os.RemoveAll(path.Join(module.updateDir, updatedFileName)); err != nil {
			return false, aoserrors.Wrap(err)
		}

		return false, nil
	}

	if module.state.UpdateState != preparedState {
		return false, aoserrors.Errorf("wrong state: %s", module.state.UpdateState)
	}

	if err = ioutil.WriteFile(path.Join(module.updateDir, doUpdateFileName),
		[]byte(module.state.UpdateType), 0644); err != nil {
		return false, aoserrors.Wrap(err)
	}

	module.state.UpdateState = updatedState

	if err = module.saveState(); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return true, nil
}

// Apply applies current update
func (module *OverlayModule) Apply() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Apply overlay module")

	// Remove updated flag
	os.Remove(path.Join(module.updateDir, updatedFileName))

	if module.bootErr != nil {
		return false, aoserrors.Wrap(module.bootErr)
	}

	if module.state.UpdateState == idleState {
		if err = module.clearUpdateDir(); err != nil {
			return false, aoserrors.Wrap(err)
		}

		return false, nil
	}

	if module.state.UpdateState != updatedState {
		return false, aoserrors.Errorf("wrong state: %s", module.state.UpdateState)
	}

	if err = ioutil.WriteFile(path.Join(module.updateDir, doApplyFileName),
		[]byte(module.state.UpdateType), 0644); err != nil {
		return false, aoserrors.Wrap(err)
	}

	module.state.UpdateState = idleState

	if err = module.saveState(); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return true, nil
}

// Revert reverts current update
func (module *OverlayModule) Revert() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Revert overlay module")

	if module.state.UpdateState == idleState {
		return false, nil
	}

	if err = module.clearUpdateDir(); err != nil {
		return false, aoserrors.Wrap(err)
	}

	module.state.UpdateState = idleState

	if err = module.saveState(); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if module.bootWithUpdate {
		rebootRequired = true
	}

	return rebootRequired, nil
}

// Reboot performs module reboot
func (module *OverlayModule) Reboot() (err error) {
	if module.rebooter != nil {
		log.WithFields(log.Fields{"id": module.id}).Debug("Reboot overlay module")

		if err = module.rebooter.Reboot(); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state updateState) String() string {
	return [...]string{"idle", "prepared", "updated"}[state]
}

func (module *OverlayModule) saveState() (err error) {
	log.WithFields(log.Fields{"id": module.id, "state": module.state.UpdateState}).Debug("Save state")

	stateJSON, err := json.Marshal(module.state)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if err = module.storage.SetModuleState(module.id, stateJSON); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (module *OverlayModule) getState() (err error) {
	stateJSON, err := module.storage.GetModuleState(module.id)
	if err != nil {
		if strings.Contains(err.Error(), database.ErrNotExistStr) {
			module.state = moduleState{}
			return nil
		}

		return aoserrors.Wrap(err)
	}

	if stateJSON != nil {
		if err = json.Unmarshal(stateJSON, &module.state); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	log.WithFields(log.Fields{"id": module.id, "state": module.state.UpdateState}).Debug("Get state")

	return nil
}

func (module *OverlayModule) getModuleVersion() (version string, err error) {
	data, err := ioutil.ReadFile(module.versionFile)
	if err != nil {
		return "", aoserrors.Errorf("nonexistent or empty vendor version file %s, err: %s", module.versionFile, err)
	}

	pattern := regexp.MustCompile(`VERSION\s*=\s*\"(.+)\"`)

	loc := pattern.FindSubmatchIndex(data)
	if loc == nil {
		return "", aoserrors.Errorf("vendor version file has wrong format")
	}

	return string(data[loc[2]:loc[3]]), nil
}

func (module *OverlayModule) clearUpdateDir() (err error) {
	if err = os.RemoveAll(module.updateDir); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = os.MkdirAll(module.updateDir, 0755); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
