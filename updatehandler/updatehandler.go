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
	"os"
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
	upgradeRevertModulesStage
	upgradeFinishStateControllerStage
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

const bundleDir = "bundleDir"
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
	Upgrade(path string) (err error)
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
	AddModuleStatus(id string, status error) (err error)
	RemoveModuleStatus(id string) (err error)
	GetModuleStatuses() (moduleStatuses map[string]error, err error)
	ClearModuleStatuses() (err error)
}

// StateController state controller interface
type StateController interface {
	GetVersion() (version uint64, err error)
	GetPlatformID() (id string, err error)
	Upgrade(version uint64, moduleIds []string) (err error)
	Revert(version uint64, moduleIds []string) (err error)
	UpgradeFinished(version uint64, status error, moduleStatus map[string]error) (postpone bool, err error)
	RevertFinished(version uint64, status error, moduleStatus map[string]error) (postpone bool, err error)
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
	handler = &Handler{
		stateController: stateController,
		storage:         storage,
		moduleProvider:  moduleProvider,
		upgradeDir:      path.Join(cfg.UpgradeDir, bundleDir),
	}

	if _, err := os.Stat(handler.upgradeDir); os.IsNotExist(err) {
		if errMkdir := os.MkdirAll(handler.upgradeDir, 0755); errMkdir != nil {
			return nil, errMkdir
		}
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

	log.WithField("imageVersion", handler.currentVersion).Debug("Create update handler")

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

	if err = handler.storage.ClearModuleStatuses(); err != nil {
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

		statusCallback := handler.statusCallaback

		handler.Unlock()
		statusCallback(status)
		handler.Lock()
	}
}

func (handler *Handler) upgrade(path string, stage int) {
	for {
		log.WithField("stage", stage).Debug("Upgrade stage changed")

		switch stage {
		case upgradeInitStage:
			if handler.lastError = os.RemoveAll(handler.upgradeDir); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			stage = upgradeUnpackStage

		case upgradeUnpackStage:
			if handler.lastError = os.MkdirAll(handler.upgradeDir, 0755); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			if handler.lastError = image.UntarGZArchive(path, handler.upgradeDir); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			stage = upgradeStartStateControllerStage

		case upgradeStartStateControllerStage:
			if handler.stateController != nil {
				if handler.lastError = handler.upgradeStateController(); handler.lastError != nil {
					stage = upgradeFinishStage
					break
				}
			}

			stage = upgradeModulesStage

		case upgradeModulesStage:
			handler.lastError = handler.updateModules()
			stage = upgradeFinishStateControllerStage

		case upgradeFinishStateControllerStage:
			if handler.stateController != nil {
				var postpone bool
				var moduleStatuses map[string]error

				if moduleStatuses, handler.lastError = handler.storage.GetModuleStatuses(); handler.lastError != nil {
					break
				}

				if postpone, handler.lastError = handler.stateController.UpgradeFinished(
					handler.operationVersion, handler.lastError, moduleStatuses); postpone {
					return
				}
			}

			if handler.lastError != nil {
				stage = upgradeRevertModulesStage
				break
			}

			stage = upgradeFinishStage

		case upgradeRevertModulesStage:
			if err := handler.revertModules(); err != nil {
				log.Errorf("Error reverting modules: %s", err)
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
				if handler.lastError = handler.revertStateController(); handler.lastError != nil {
					stage = upgradeFinishStage
					break
				}
			}

			stage = revertModulesStage

		case revertModulesStage:
			if handler.lastError = handler.revertModules(); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			stage = revertFinishStateControllerStage

		case revertFinishStateControllerStage:
			if handler.stateController != nil {
				var postpone bool
				var moduleStatuses map[string]error

				if moduleStatuses, handler.lastError = handler.storage.GetModuleStatuses(); handler.lastError != nil {
					break
				}

				if postpone, handler.lastError = handler.stateController.RevertFinished(
					handler.operationVersion, handler.lastError, moduleStatuses); postpone {
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
		}

		if err := handler.storage.SetOperationStage(stage); err != nil {
			handler.operationFinished(revertedState, err)
			return
		}
	}
}

func (handler *Handler) getImageMetadata() (metadata imageMetadata, err error) {
	metadataJSON, err := ioutil.ReadFile(path.Join(handler.upgradeDir, metadataFileName))
	if err != nil {
		return metadata, err
	}

	if err = json.Unmarshal(metadataJSON, &metadata); err != nil {
		return metadata, err
	}

	return metadata, err
}

func (handler *Handler) upgradeStateController() (err error) {
	metadata, err := handler.getImageMetadata()
	if err != nil {
		return err
	}

	moduleIds := make([]string, 0, len(metadata.UpdateItems))

	for _, item := range metadata.UpdateItems {
		moduleIds = append(moduleIds, item.Type)
	}

	if err = handler.stateController.Upgrade(handler.operationVersion, moduleIds); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) revertStateController() (err error) {
	moduleStatuses, err := handler.storage.GetModuleStatuses()
	if err != nil {
		return err
	}

	moduleIds := make([]string, 0, len(moduleStatuses))

	for id := range moduleStatuses {
		moduleIds = append(moduleIds, id)
	}

	if err = handler.stateController.Revert(handler.operationVersion, moduleIds); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) updateModule(id, path string) (err error) {
	log.WithField("id", id).Info("Update module")

	module, err := handler.moduleProvider.GetModuleByID(id)
	if err != nil {
		return err
	}

	updateModule, ok := module.(UpdateModule)
	if !ok {
		return fmt.Errorf("module %s doesn't implement update interface", id)
	}

	if err = updateModule.Upgrade(path); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) updateModules() (err error) {
	metadata, err := handler.getImageMetadata()
	if err != nil {
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

	moduleStatuses, err := handler.storage.GetModuleStatuses()
	if err != nil {
		return err
	}

	for _, item := range metadata.UpdateItems {
		status, ok := moduleStatuses[item.Type]
		if ok {
			continue
		}

		if status != nil {
			return status
		}

		status = handler.updateModule(item.Type, path.Join(handler.upgradeDir, item.Path))

		if err = handler.storage.AddModuleStatus(item.Type, status); err != nil {
			return err
		}
	}

	return nil
}

func (handler *Handler) revertModule(id string) (err error) {
	log.WithField("id", id).Debug("Revert module")

	module, err := handler.moduleProvider.GetModuleByID(id)
	if err != nil {
		return err
	}

	updateModule, ok := module.(UpdateModule)
	if !ok {
		return fmt.Errorf("module %s doesn't implement update interface", id)
	}

	if err = updateModule.Revert(); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) revertModules() (err error) {
	moduleStatuses, err := handler.storage.GetModuleStatuses()
	if err != nil {
		return err
	}

	for id, status := range moduleStatuses {
		if status != nil {
			continue
		}

		status = handler.revertModule(id)

		if err == nil {
			err = status
		}

		if err := handler.storage.RemoveModuleStatus(id); err != nil {
			log.Errorf("Can't remove module status: %s", err)
		}
	}

	return err
}
