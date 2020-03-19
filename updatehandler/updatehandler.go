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
	upgradeModulesStage
	upgradeRevertModulesStage
	upgradeFinishStage
)

// Revert stages
const (
	revertInitStage = iota
	revertModulesStage
	revertFinishStage
)

const bundleDir = "bundleDir"
const metadataFileName = "metadata.json"

const statusChannelSize = 1

/*******************************************************************************
 * Types
 ******************************************************************************/

// Handler update handler
type Handler struct {
	sync.Mutex
	platform PlatformController
	storage  StateStorage
	modules  map[string]UpdateModule

	upgradeDir       string
	bundleDir        string
	state            handlerState
	currentVersion   uint64
	operationVersion uint64
	lastError        error

	statusChannel chan string
}

// UpdateModule interface for module plugin
type UpdateModule interface {
	// GetID returns module ID
	GetID() (id string)
	// Init initializes module
	Init() (err error)
	// Upgrade upgrades module
	Upgrade(version uint64, imagePath string) (rebootRequired bool, err error)
	// CancelUpgrade cancels upgrade
	CancelUpgrade(version uint64) (rebootRequired bool, err error)
	// FinishUpgrade finished upgrade
	FinishUpgrade(version uint64) (err error)
	// Revert reverts module
	Revert(version uint64) (rebootRequired bool, err error)
	// CancelRevert cancels revert module
	CancelRevert(version uint64) (rebootRequired bool, err error)
	// FinishRevert finished revert
	FinishRevert(version uint64) (err error)
	// Close closes update module
	Close() (err error)
}

// StateStorage provides API to store/retreive persistent data
type StateStorage interface {
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

// PlatformController platform controller
type PlatformController interface {
	GetVersion() (version uint64, err error)
	SetVersion(version uint64) (err error)
	GetPlatformID() (id string, err error)
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

type handlerState int
type upgradeStage int
type revertStage int

/*******************************************************************************
 * Public
 ******************************************************************************/

// New returns pointer to new Handler
func New(cfg *config.Config, modules []UpdateModule,
	platform PlatformController, storage StateStorage) (handler *Handler, err error) {
	handler = &Handler{
		platform:      platform,
		storage:       storage,
		upgradeDir:    cfg.UpgradeDir,
		bundleDir:     path.Join(cfg.UpgradeDir, bundleDir),
		statusChannel: make(chan string, statusChannelSize),
	}

	if _, err := os.Stat(handler.bundleDir); os.IsNotExist(err) {
		if errMkdir := os.MkdirAll(handler.bundleDir, 0755); errMkdir != nil {
			return nil, errMkdir
		}
	}

	if handler.currentVersion, err = handler.platform.GetVersion(); err != nil {
		return nil, err
	}

	log.WithField("imageVersion", handler.currentVersion).Debug("Create update handler")

	state, err := handler.storage.GetState()
	if err != nil {
		return nil, err
	}

	handler.state = handlerState(state)

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

	handler.modules = make(map[string]UpdateModule)

	for _, module := range modules {
		handler.modules[module.GetID()] = module
	}

	if handler.state == upgradingState {
		go handler.upgrade(imagePath, upgradeStage(stage))
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

	if version <= handler.currentVersion {
		return fmt.Errorf("wrong update version: %d", version)
	}

	if handler.state != revertedState && handler.state != upgradedState {
		return errors.New("wrong state")
	}

	if handler.state == revertedState && handler.lastError != nil {
		return errors.New("can't upgrade after failed revert")
	}

	imagePath := path.Join(handler.upgradeDir, imageInfo.Path)

	if err = image.CheckFileInfo(imagePath, image.FileInfo{
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

	if err = handler.storage.SetImagePath(imagePath); err != nil {
		return err
	}

	if err = handler.storage.ClearModuleStatuses(); err != nil {
		return err
	}

	handler.state = upgradingState

	if err = handler.storage.SetState(int(handler.state)); err != nil {
		return err
	}

	go handler.upgrade(imagePath, upgradeInitStage)

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

	if err = handler.storage.SetState(int(handler.state)); err != nil {
		return err
	}

	go handler.revert(revertInitStage)

	return nil
}

// StatusChannel this channel is used to notify when upgrade/revert is finished
func (handler *Handler) StatusChannel() (statusChannel <-chan string) {
	return handler.statusChannel
}

// Close closes update handler
func (handler *Handler) Close() {
	close(handler.statusChannel)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (stage upgradeStage) String() string {
	return [...]string{
		"Init", "Unpack", "UpgradeModules",
		"RevertModules", "Finish"}[stage]
}

func (stage revertStage) String() string {
	return [...]string{
		"Init", "RevertModules", "Finish"}[stage]
}

func (state handlerState) String() string {
	return [...]string{
		"Upgraded", "Upgrading", "Reverted", "Reverting"}[state]
}

func (handler *Handler) operationFinished(newState handlerState, operationError error) {
	handler.Lock()
	defer handler.Unlock()

	if operationError != nil {
		log.Errorf("Operation failed: %s", operationError)
	} else {
		log.WithFields(log.Fields{"newState": newState}).Info("Operation finished")
		handler.currentVersion = handler.operationVersion
	}

	handler.state = newState

	if err := handler.storage.SetState(int(handler.state)); err != nil {
		log.Errorf("Can't set state: %s", err)
	}

	if err := handler.platform.SetVersion(handler.currentVersion); err != nil {
		log.Errorf("Can't set current version: %s", err)
	}

	status := umprotocol.SuccessStatus

	if operationError != nil {
		status = umprotocol.FailedStatus
	}

	handler.statusChannel <- status
}

func (handler *Handler) upgrade(path string, stage upgradeStage) {
	for {
		log.WithField("stage", stage).Debug("Upgrade stage changed")

		switch stage {
		case upgradeInitStage:
			if handler.lastError = os.RemoveAll(handler.bundleDir); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			stage = upgradeUnpackStage

		case upgradeUnpackStage:
			if handler.lastError = os.MkdirAll(handler.bundleDir, 0755); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			if handler.lastError = image.UntarGZArchive(path, handler.bundleDir); handler.lastError != nil {
				stage = upgradeFinishStage
				break
			}

			stage = upgradeModulesStage

		case upgradeModulesStage:
			if handler.lastError = handler.updateModules(handler.operationVersion); handler.lastError != nil {
				stage = upgradeRevertModulesStage
				break
			}

			stage = upgradeFinishStage

		case upgradeRevertModulesStage:
			if err := handler.revertModules(handler.currentVersion); err != nil {
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

		if err := handler.storage.SetOperationStage(int(stage)); err != nil {
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
			stage = revertModulesStage

		case revertModulesStage:
			if handler.lastError = handler.revertModules(handler.operationVersion); handler.lastError != nil {
				stage = revertFinishStage
				break
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
	metadataJSON, err := ioutil.ReadFile(path.Join(handler.bundleDir, metadataFileName))
	if err != nil {
		return metadata, err
	}

	if err = json.Unmarshal(metadataJSON, &metadata); err != nil {
		return metadata, err
	}

	return metadata, err
}

func (handler *Handler) updateModule(version uint64, id, path string) (err error) {
	log.WithField("id", id).Info("Update module")

	module, ok := handler.modules[id]
	if !ok {
		return errors.New("module not found")
	}

	if _, err = module.Upgrade(version, path); err != nil {
		log.WithField("id", id).Errorf("Update module failed: %s", err)
		return err
	}

	return nil
}

func (handler *Handler) updateModules(version uint64) (err error) {
	metadata, err := handler.getImageMetadata()
	if err != nil {
		return err
	}

	platformID, err := handler.platform.GetPlatformID()
	if err != nil {
		return err
	}

	if metadata.PlatformID != platformID {
		return errors.New("wrong platform ID")
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

		status = handler.updateModule(version, item.Type, path.Join(handler.bundleDir, item.Path))

		err = handler.storage.AddModuleStatus(item.Type, status)

		if status != nil {
			return status
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (handler *Handler) revertModule(version uint64, id string) (err error) {
	log.WithField("id", id).Info("Revert module")

	module, ok := handler.modules[id]
	if !ok {
		return errors.New("module not found")
	}
	if _, err = module.Revert(version); err != nil {
		log.WithField("id", id).Errorf("Revert module failed: %s", err)
		return err
	}

	return nil
}

func (handler *Handler) revertModules(version uint64) (err error) {
	moduleStatuses, err := handler.storage.GetModuleStatuses()
	if err != nil {
		return err
	}

	for id, status := range moduleStatuses {
		if status != nil {
			continue
		}

		status = handler.revertModule(version, id)

		if err == nil {
			err = status
		}

		if err := handler.storage.RemoveModuleStatus(id); err != nil {
			log.Errorf("Can't remove module status: %s", err)
		}
	}

	return err
}
