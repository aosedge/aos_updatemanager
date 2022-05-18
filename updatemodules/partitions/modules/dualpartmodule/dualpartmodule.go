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

package dualpartmodule

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"syscall"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/partition"
	"github.com/aoscloud/aos_common/utils/fs"
	log "github.com/sirupsen/logrus"

	"github.com/aoscloud/aos_updatemanager/updatehandler"
)

// The sequence diagram of update:
//
// * Init()                               module initialization
//
// * Prepare(imagePath)                   prepare image before update, check
//                                        update integrity.
//
// * Update()                             update fallback partition
//                                        set it as next boot and set reboot flag
//
// * Reboot()                             Reboot if reboot flag was set
//------------------------------- Reboot ---------------------------------------
//
// * Update()                             check status after reboot, switch
//                                        to updated partition (put it to the
//                                        first place in boot order)
//
// * Apply()                              return status and start same update
//                                        on other partition, if it fails try to
//                                        copy content from current partition
//
// * Reboot()                             Reboot if reboot flag was set
//------------------------------------------------------------------------------
//
// Revert() reverts update and return the system to the previous state.
// It requests the reboot if the system is booted from the updated partition:
//
// * Revert()                              switch back to the previous
//                                         partition (make it active and
//                                         restore boot order), mark
//                                         second partition as inactive,
//                                         set rebootFlag also
//
// * Reboot()                              Reboot if reboot flag was set
//------------------------------- Reboot ---------------------------------------

/*******************************************************************************
 * Constants
 ******************************************************************************/

const (
	idleState = iota
	preparedState
	updatedState
)

const numPartitions = 2

/*******************************************************************************
 * Types
 ******************************************************************************/

// RebootHandler handler for the reboot command.
type RebootHandler interface {
	Reboot() (err error)
}

// UpdateChecker handler for checking update.
type UpdateChecker interface {
	Check() (err error)
}

// DualPartModule update dual partition module.
type DualPartModule struct {
	id string

	storage          Storage
	controller       StateController
	rebootHandler    RebootHandler
	checker          UpdateChecker
	partitions       []string
	currentPartition int
	state            moduleState
	versionFile      string
	vendorVersion    string
	bootErr          error
}

// StateController state controller interface.
type StateController interface {
	GetCurrentBoot() (index int, err error)
	GetMainBoot() (index int, err error)
	SetMainBoot(index int) (err error)
	SetBootOK() (err error)
	Close()
}

// Storage storage interface.
type Storage interface {
	GetModuleState(id string) (state []byte, err error)
	SetModuleState(id string, state []byte) (err error)
}

type moduleState struct {
	State           updateState `json:"state"`
	UpdatePartition int         `json:"updatePartition"`
	ImagePath       string      `json:"imagePath"`
}

type updateState int

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates fs update module instance.
func New(id string, partitions []string, versionFile string, controller StateController,
	storage updatehandler.ModuleStorage, rebootHandler RebootHandler,
	checker UpdateChecker,
) (updateModule updatehandler.UpdateModule, err error) {
	log.WithField("module", id).Debug("Create dualpart module")

	module := &DualPartModule{
		id:            id,
		partitions:    partitions,
		controller:    controller,
		storage:       storage,
		rebootHandler: rebootHandler,
		checker:       checker,
		versionFile:   versionFile,
	}

	if len(partitions) != numPartitions {
		return nil, aoserrors.New("num of configured partitions should be 2")
	}

	return module, nil
}

// Close closes DualPartModule.
func (module *DualPartModule) Close() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Close dualpart module")

	module.controller.Close()

	return nil
}

// GetID returns module ID.
func (module *DualPartModule) GetID() (id string) {
	return module.id
}

// Init initializes module.
func (module *DualPartModule) Init() (err error) {
	defer func() {
		if err != nil && module.bootErr == nil {
			module.bootErr = aoserrors.Wrap(err)
		}

		if module.bootErr != nil {
			log.WithFields(log.Fields{"id": module.id}).Errorf("Module boot error: %s", module.bootErr)
		}
	}()

	log.WithFields(log.Fields{"id": module.id}).Debug("Init dualpart module")

	if err = module.controller.SetBootOK(); err != nil {
		return aoserrors.Wrap(err)
	}

	if module.currentPartition, err = module.controller.GetCurrentBoot(); err != nil {
		return aoserrors.Wrap(err)
	}

	primaryPartition, err := module.controller.GetMainBoot()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if err = module.getState(); err != nil {
		return aoserrors.Wrap(err)
	}

	if module.state.State == idleState && module.currentPartition != primaryPartition {
		log.WithFields(log.Fields{"id": module.id}).Warn("Boot from fallback partition")
	}

	if module.vendorVersion, err = module.getModuleVersion(module.partitions[module.currentPartition]); err != nil {
		return aoserrors.Wrap(err)
	}

	if module.checker != nil && module.bootErr == nil {
		module.bootErr = aoserrors.Wrap(module.checker.Check())
	}

	return nil
}

// GetVendorVersion returns vendor version.
func (module *DualPartModule) GetVendorVersion() (version string, err error) {
	return module.vendorVersion, nil
}

// Prepare preparing image.
func (module *DualPartModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	log.WithFields(log.Fields{
		"id":            module.id,
		"imagePath":     imagePath,
		"vendorVersion": vendorVersion,
	}).Debug("Prepare dualpart module")

	if module.state.State != idleState && module.state.State != preparedState {
		return aoserrors.Errorf("wrong state during Prepare command. Expected %d, got %d", idleState,
			module.state.State)
	}

	if _, err = os.Stat(imagePath); err != nil {
		return aoserrors.Wrap(err)
	}

	module.state.ImagePath = imagePath

	if err = module.setState(preparedState); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// Update updates module.
func (module *DualPartModule) Update() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Update dualpart module")

	if module.state.State == updatedState {
		log.Debugf("Current partition %d, update partition = %d", module.currentPartition, module.state.UpdatePartition)

		if module.currentPartition != module.state.UpdatePartition {
			return false, aoserrors.Errorf("update was failed")
		}

		if module.bootErr != nil {
			return false, aoserrors.Wrap(module.bootErr)
		}

		return false, nil
	}

	if module.state.State != preparedState {
		return false, aoserrors.Errorf("wrong state during Update command. Expected %d, got %d", preparedState,
			module.state.State)
	}

	secPartition := (module.currentPartition + 1) % len(module.partitions)

	module.state.UpdatePartition = secPartition

	if _, err = partition.CopyFromGzipArchive(module.partitions[secPartition], module.state.ImagePath); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.controller.SetMainBoot(secPartition); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.setState(updatedState); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return true, nil
}

// Revert reverts update.
func (module *DualPartModule) Revert() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Revert dualpart module")

	if module.state.State == idleState {
		return false, nil
	}

	updatePartition := module.state.UpdatePartition
	secPartition := (updatePartition + 1) % len(module.partitions)

	if _, err = partition.Copy(module.partitions[updatePartition], module.partitions[secPartition]); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.controller.SetMainBoot(secPartition); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.setState(idleState); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if module.currentPartition == module.state.UpdatePartition {
		rebootRequired = true
	}

	return rebootRequired, nil
}

// Apply applies update.
func (module *DualPartModule) Apply() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Apply dualpart module")

	// Skip if update was already applied
	if module.state.State == idleState {
		return false, nil
	}

	if module.state.State != updatedState {
		return false, aoserrors.Errorf("wrong state during Apply command. Expected %d, got %d", updatedState,
			module.state.State)
	}

	currentPartition := module.state.UpdatePartition
	secPartition := (currentPartition + 1) % len(module.partitions)

	if _, err = partition.Copy(module.partitions[secPartition], module.partitions[currentPartition]); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.setState(idleState); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return false, nil
}

// Reboot performs module reboot.
func (module *DualPartModule) Reboot() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debugf("Reboot dualpart module")

	if module.rebootHandler == nil {
		return nil
	}

	// Close controller before reboot
	module.controller.Close()

	return aoserrors.Wrap(module.rebootHandler.Reboot())
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state updateState) String() string {
	return [...]string{"idle", "prepared", "updated"}[state]
}

func (module *DualPartModule) getState() (err error) {
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

func (module *DualPartModule) setState(state updateState) (err error) {
	log.WithFields(log.Fields{"id": module.id, "state": state}).Debugf("State changed")

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

func (module *DualPartModule) getModuleVersion(part string) (version string, err error) {
	mountDir, err := ioutil.TempDir("", "aos_")
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	defer os.RemoveAll(mountDir)

	partInfo, err := partition.GetPartInfo(part)
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	if err = fs.Mount(partInfo.Device, mountDir, partInfo.FSType, syscall.MS_RDONLY, ""); err != nil {
		return "", aoserrors.Wrap(err)
	}

	defer func() {
		if err := fs.Umount(mountDir); err != nil {
			log.Errorf("can't unmount partitions: %s", aoserrors.Wrap(err))
		}
	}()

	versionFilePath := path.Join(mountDir, module.versionFile)

	data, err := ioutil.ReadFile(versionFilePath)
	if err != nil {
		return "", aoserrors.Errorf("nonexistent or empty vendor version file %s, err: %s", versionFilePath, err)
	}

	pattern := regexp.MustCompile(`VERSION\s*=\s*\"(.+)\"`)

	loc := pattern.FindSubmatchIndex(data)
	if loc == nil {
		return "", aoserrors.Errorf("vendor version file has wrong format")
	}

	return string(data[loc[2]:loc[3]]), nil
}
