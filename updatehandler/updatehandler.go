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

package updatehandler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"sync"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/nunc-ota/aos_common/image"
	"gitpct.epam.com/nunc-ota/aos_common/umprotocol"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

// States
const (
	upgradedState = iota
	upgradingState
	revertedState
	revertingState
)

// Upgrade stages
const (
	upgradeInitStage = iota
	upgradeUnpackStage
	upgradeStartStateControllerStage
	upgradeModulesStage
	upgradeFinishStateControllerStage
	revertModuleStage
	upgradeFinishStage
)

// Revert stages
const (
	revertInitStage = iota
	revertStartStateControllerStage
	revertModulesStage
	revertFinishStateControllerStage
	revertFinishStage
)

const metadataFileName = "metadata.json"

/*******************************************************************************
 * Types
 ******************************************************************************/

// Handler update handler
type Handler struct {
	sync.Mutex
	stateController StateController
	storage         Storage
	moduleProvider  ModuleProvider

	upgradeDir       string
	state            int
	currentVersion   uint64
	operationVersion uint64
	lastError        error

	statusCallaback func(status string)
}

// UpdateModule interface for module plugin
type UpdateModule interface {
	// GetID returns module ID
	GetID() (id string)
	// Upgrade upgrade module
	Upgrade(fileName string) (err error)
	// Revert revert module
	Revert() (err error)
}

// ModuleProvider module provider interface
type ModuleProvider interface {
	// GetModuleByID returns module by id
	GetModuleByID(id string) (module interface{}, err error)
}

// Storage provides API to store/retreive persistent data
type Storage interface {
	SetState(state int) (err error)
	GetState() (state int, err error)
	SetImagePath(path string) (err error)
	GetImagePath() (path string, err error)
	SetOperationStage(stage int) (err error)
	GetOperationStage() (stage int, err error)
	SetCurrentVersion(version uint64) (err error)
	GetCurrentVersion() (version uint64, err error)
	SetOperationVersion(version uint64) (err error)
	GetOperationVersion() (version uint64, err error)
	SetLastError(lastError error) (err error)
	GetLastError() (lastError error, err error)
}

// StateController state controller interface
type StateController interface {
	GetVersion() (version uint64, err error)
	GetPlatformID() (id string, err error)
	Upgrade(version uint64) (err error)
	Revert(version uint64) (err error)
	UpgradeFinished(version uint64, moduleStatus map[string]error) (postpone bool, err error)
	RevertFinished(version uint64, moduleStatus map[string]error) (postpone bool, err error)
}

type itemMetadata struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type imageMetadata struct {
	PlatformID        string         `json:"platformId"`
	BundleVersion     string         `json:"bundleVersion,omitempty"`
	BundleDescription string         `json:"bundleDescription,omitempty"`
	UpdateItems       []itemMetadata `json:"updateItems"`
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New returns pointer to new Handler
func New(cfg *config.Config, moduleProvider ModuleProvider, stateController StateController, storage Storage) (handler *Handler, err error) {
	log.Debug("Create update handler")

	handler = &Handler{
		stateController: stateController,
		storage:         storage,
		moduleProvider:  moduleProvider,
		upgradeDir:      cfg.UpgradeDir,
	}

	if handler.stateController != nil {
		if handler.currentVersion, err = handler.stateController.GetVersion(); err != nil {
			return nil, err
		}
	} else {
		if handler.currentVersion, err = storage.GetCurrentVersion(); err != nil {
			return nil, err
		}
	}

	if handler.state, err = handler.storage.GetState(); err != nil {
		return nil, err
	}

	if handler.operationVersion, err = handler.storage.GetOperationVersion(); err != nil {
		return nil, err
	}

	if handler.lastError, err = handler.storage.GetLastError(); err != nil {
		return nil, err
	}

	imagePath, err := handler.storage.GetImagePath()
	if err != nil {
		return nil, err
	}

	stage, err := handler.storage.GetOperationStage()
	if err != nil {
		return nil, err
	}

	if handler.state == upgradingState {
		go handler.upgrade(imagePath, stage)
	}

	if handler.state == revertingState {
		go handler.revert(stage)
	}

	return handler, nil
}

// GetCurrentVersion returns current system version
func (handler *Handler) GetCurrentVersion() (version uint64) {
	handler.Lock()
	defer handler.Unlock()

	return handler.currentVersion
}

// GetOperationVersion returns upgrade/revert version
func (handler *Handler) GetOperationVersion() (version uint64) {
	handler.Lock()
	defer handler.Unlock()

	return handler.operationVersion
}

// GetStatus returns update status
func (handler *Handler) GetStatus() (status string) {
	handler.Lock()
	defer handler.Unlock()

	status = umprotocol.SuccessStatus

	if handler.state == revertingState || handler.state == upgradingState {
		status = umprotocol.InProgressStatus
	}

	if handler.lastError != nil {
		status = umprotocol.FailedStatus
	}

	return status
}

// GetLastOperation returns last operation
func (handler *Handler) GetLastOperation() (operation string) {
	handler.Lock()
	defer handler.Unlock()

	if handler.state == revertingState || handler.state == revertedState {
		operation = umprotocol.RevertOperation
	}

	if handler.state == upgradingState || handler.state == upgradedState {
		operation = umprotocol.UpgradeOperation
	}

	return operation
}

// GetLastError returns last upgrade error
func (handler *Handler) GetLastError() (err error) {
	handler.Lock()
	defer handler.Unlock()

	return handler.lastError
}

// Upgrade performs upgrade operation
func (handler *Handler) Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithField("version", version).Info("Upgrade")

	if handler.state != revertedState && handler.state != upgradedState {
		return errors.New("wrong state")
	}

	if handler.state == revertedState && handler.lastError != nil {
		return errors.New("can't upgrade after failed revert")
	}

	if err = image.CheckFileInfo(imageInfo.Path, image.FileInfo{
		Sha256: imageInfo.Sha256,
		Sha512: imageInfo.Sha512,
		Size:   imageInfo.Size}); err != nil {
		return err
	}

	handler.operationVersion = version

	if err = handler.storage.SetOperationVersion(handler.operationVersion); err != nil {
		return err
	}

	handler.lastError = nil

	if err = handler.storage.SetLastError(handler.lastError); err != nil {
		return err
	}

	if err = handler.storage.SetOperationStage(upgradeInitStage); err != nil {
		return err
	}

	if err = handler.storage.SetImagePath(imageInfo.Path); err != nil {
		return err
	}

	handler.state = upgradingState

	if err = handler.storage.SetState(handler.state); err != nil {
		return err
	}

	go handler.upgrade(imageInfo.Path, upgradeInitStage)

	return nil
}

// Revert performs revert operation
func (handler *Handler) Revert(version uint64) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithField("version", version).Info("Revert")

	if !(handler.state == upgradedState && handler.lastError == nil) {
		return errors.New("wrong state")
	}

	handler.operationVersion = version

	if err = handler.storage.SetOperationVersion(handler.operationVersion); err != nil {
		return err
	}

	handler.lastError = nil

	if err = handler.storage.SetLastError(handler.lastError); err != nil {
		return err
	}

	if err = handler.storage.SetOperationStage(revertInitStage); err != nil {
		return err
	}

	handler.state = revertingState

	if err = handler.storage.SetState(handler.state); err != nil {
		return err
	}

	go handler.revert(revertInitStage)

	return nil
}

// SetStatusCallback sets callback which will be called when upgrade/revert is finished
func (handler *Handler) SetStatusCallback(callback func(status string)) {
	handler.Lock()
	defer handler.Unlock()

	handler.statusCallaback = callback
}

// Close closes update handler
func (handler *Handler) Close() {
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (handler *Handler) operationFinished(newState int, operationError error) {
	handler.Lock()
	defer handler.Unlock()

	if operationError != nil {
		log.Errorf("Operation failed: %s", operationError)
	} else {
		log.WithFields(log.Fields{"newState": newState}).Info("Operation finished")
	}

	handler.state = newState

	if err := handler.storage.SetState(handler.state); err != nil {
		log.Errorf("Can't set state: %s", err)
	}

	if handler.stateController != nil {
		var err error

		if handler.currentVersion, err = handler.stateController.GetVersion(); err != nil {
			log.Errorf("Can't get current version: %s", err)
		}
	} else if operationError == nil {
		handler.currentVersion = handler.operationVersion
	}

	if err := handler.storage.SetCurrentVersion(handler.currentVersion); err != nil {
		log.Errorf("Can't set current version: %s", err)
	}

	if handler.statusCallaback != nil {
		status := umprotocol.SuccessStatus

		if operationError != nil {
			status = umprotocol.FailedStatus
		}

		handler.statusCallaback(status)
	}
}

func (handler *Handler) upgrade(path string, stage int) {
	for {
		log.WithField("stage", stage).Debug("Upgrade stage changed")

		switch stage {
		case upgradeInitStage:
			stage = upgradeUnpackStage

		case upgradeUnpackStage:
			handler.lastError = image.UntarGZArchive(path, handler.upgradeDir)
			stage = upgradeStartStateControllerStage

		case upgradeStartStateControllerStage:
			if handler.stateController != nil {
				handler.lastError = handler.stateController.Upgrade(handler.operationVersion)
			}

			stage = upgradeModulesStage

		case upgradeModulesStage:
			handler.lastError = handler.updateModules()
			stage = upgradeFinishStateControllerStage

		case upgradeFinishStateControllerStage:
			if handler.stateController != nil {
				var postpone bool

				postpone, handler.lastError = handler.stateController.UpgradeFinished(handler.operationVersion, nil)
				if postpone {
					return
				}
			}

			stage = upgradeFinishStage

		case upgradeFinishStage:
			handler.operationFinished(upgradedState, handler.lastError)
			return
		}

		if handler.lastError != nil {
			if err := handler.storage.SetLastError(handler.lastError); err != nil {
				handler.operationFinished(upgradedState, err)
				return
			}

			stage = upgradeFinishStage
		}

		if err := handler.storage.SetOperationStage(stage); err != nil {
			handler.operationFinished(upgradedState, err)
			return
		}
	}
}

func (handler *Handler) revert(stage int) {
	for {
		log.WithField("stage", stage).Debug("Revert stage changed")

		switch stage {
		case revertInitStage:
			stage = revertStartStateControllerStage

		case revertStartStateControllerStage:
			if handler.stateController != nil {
				handler.lastError = handler.stateController.Revert(handler.operationVersion)
			}

			stage = revertModulesStage

		case revertModulesStage:
			stage = revertFinishStateControllerStage

		case revertFinishStateControllerStage:
			if handler.stateController != nil {
				var postpone bool

				postpone, handler.lastError = handler.stateController.RevertFinished(handler.operationVersion, nil)
				if postpone {
					return
				}
			}

			stage = revertFinishStage

		case revertFinishStage:
			handler.operationFinished(revertedState, handler.lastError)
			return
		}

		if handler.lastError != nil {
			if err := handler.storage.SetLastError(handler.lastError); err != nil {
				handler.operationFinished(revertedState, err)
				return
			}

			stage = revertFinishStage
		}

		if err := handler.storage.SetOperationStage(stage); err != nil {
			handler.operationFinished(revertedState, err)
			return
		}
	}
}

func (handler *Handler) updateModules() (err error) {
	metadataJSON, err := ioutil.ReadFile(path.Join(handler.upgradeDir, metadataFileName))
	if err != nil {
		return err
	}

	var metadata imageMetadata

	if err = json.Unmarshal(metadataJSON, &metadata); err != nil {
		return err
	}

	if handler.stateController != nil {
		platformID, err := handler.stateController.GetPlatformID()
		if err != nil {
			return err
		}

		if metadata.PlatformID != platformID {
			return errors.New("wrong platform ID")
		}
	}

	for _, item := range metadata.UpdateItems {
		log.WithField("id", item.Type).Debug("Update module")

		module, err := handler.moduleProvider.GetModuleByID(item.Type)
		if err != nil {
			return err
		}

		updateModule, ok := module.(UpdateModule)
		if !ok {
			return fmt.Errorf("module %s doesn't implement update interface", item.Type)
		}

		if err = updateModule.Upgrade(item.Path); err != nil {
			return err
		}
	}

	return nil
}

func (handler *Handler) getModuleByID(id string) (module UpdateModule, err error) {
	providedModule, err := handler.moduleProvider.GetModuleByID(id)
	if err != nil {
		return nil, err
	}

	updateModule, ok := providedModule.(UpdateModule)
	if !ok {
		return nil, fmt.Errorf("module %s doesn't provide required interface", id)
	}

	return updateModule, nil
}
