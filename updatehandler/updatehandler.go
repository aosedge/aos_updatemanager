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

// Operations type
const (
	upgradeOperation = iota
	revertOperation
)

//
// Upgrade state machine:
//
// idleState          -> upgrade request                     -> initState
// initState          -> unpack image                        -> upgradeState
// upgradeState       -> upgrade modules                     -> finishState
// finishState        -> send status                         -> idleState
//
// If some error happens during upgrade modules:
//
// upgradeState       -> upgrade modules fails               -> cancelState
// cancelUpgradeState -> cancel upgrade modules, send status -> idleState
//
// Revert state machine:
//
// idleState          -> revert request                      -> initState
// initState          ->                                     -> revertState
// revertState        -> revert modules                      -> finishState
// finishState        -> send status                         -> idleState
//
// If some error happens during revert modules:
//
// revertState        -> revert modules fails                -> cancelState
// cancelRevertState  -> cancel revert modules, send status  -> idleState
//

const (
	idleState = iota
	initState
	upgradeState
	revertState
	cancelState
	finishState
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

	upgradeDir     string
	bundleDir      string
	state          handlerState
	currentVersion uint64

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
	SetOperationState(jsonState []byte) (err error)
	GetOperationState() (jsonState []byte, err error)
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

type handlerState struct {
	OperationType    operationType  `json:"operationType"`
	OperationState   operationState `json:"operationState"`
	OperationVersion uint64         `json:"operationVersion"`
	ImagePath        string         `json:"imagePath"`
	LastError        error          `json:"-"`
	ErrorMsg         string         `json:"errorMsg"`
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

type operationType int
type operationState int

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

	if err = handler.getState(); err != nil {
		return nil, err
	}

	handler.modules = make(map[string]UpdateModule)

	for _, module := range modules {
		handler.modules[module.GetID()] = module
	}

	if handler.state.OperationState != idleState {
		if handler.state.OperationType == upgradeOperation {
			go handler.upgrade()
		}

		if handler.state.OperationType == revertOperation {
			go handler.revert()
		}
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

	return handler.state.OperationVersion
}

// GetStatus returns update status
func (handler *Handler) GetStatus() (status string) {
	handler.Lock()
	defer handler.Unlock()

	status = umprotocol.SuccessStatus

	if handler.state.OperationState != idleState {
		status = umprotocol.InProgressStatus
	}

	if handler.state.LastError != nil {
		status = umprotocol.FailedStatus
	}

	return status
}

// GetLastOperation returns last operation
func (handler *Handler) GetLastOperation() (operation string) {
	handler.Lock()
	defer handler.Unlock()

	if handler.state.OperationType == revertOperation {
		operation = umprotocol.RevertOperation
	}

	if handler.state.OperationType == upgradeOperation {
		operation = umprotocol.UpgradeOperation
	}

	return operation
}

// GetLastError returns last upgrade error
func (handler *Handler) GetLastError() (err error) {
	handler.Lock()
	defer handler.Unlock()

	return handler.state.LastError
}

// Upgrade performs upgrade operation
func (handler *Handler) Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithField("version", version).Info("Upgrade")

	if version <= handler.currentVersion {
		return fmt.Errorf("wrong upgrade version: %d", version)
	}

	if handler.state.OperationState != idleState {
		return errors.New("wrong state")
	}

	if handler.state.OperationType == revertOperation && handler.state.LastError != nil {
		return errors.New("can't upgrade after failed revert")
	}

	imagePath := path.Join(handler.upgradeDir, imageInfo.Path)

	if err = image.CheckFileInfo(imagePath, image.FileInfo{
		Sha256: imageInfo.Sha256,
		Sha512: imageInfo.Sha512,
		Size:   imageInfo.Size}); err != nil {
		return err
	}

	if err = handler.storage.ClearModuleStatuses(); err != nil {
		return err
	}

	handler.state.OperationState = initState
	handler.state.OperationVersion = version
	handler.state.OperationType = upgradeOperation
	handler.state.LastError = nil
	handler.state.ImagePath = imagePath

	if err = handler.setState(); err != nil {
		return err
	}

	go handler.upgrade()

	return nil
}

// Revert performs revert operation
func (handler *Handler) Revert(version uint64) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithField("version", version).Info("Revert")

	if !(handler.state.OperationType == upgradeOperation && handler.state.LastError == nil) {
		return errors.New("wrong state")
	}

	handler.state.OperationState = initState
	handler.state.OperationVersion = version
	handler.state.OperationType = revertOperation
	handler.state.LastError = nil

	if err = handler.setState(); err != nil {
		return err
	}

	go handler.revert()

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

func (opType operationType) String() string {
	return [...]string{
		"upgrade", "revert"}[opType]
}

func (state operationState) String() string {
	return [...]string{
		"idle", "init", "upgrade", "revert", "cancel", "finish"}[state]
}

func (handler *Handler) getState() (err error) {
	jsonState, err := handler.storage.GetOperationState()
	if err != nil {
		return err
	}

	if err = json.Unmarshal(jsonState, &handler.state); err != nil {
		return err
	}

	if handler.state.ErrorMsg != "" {
		handler.state.LastError = errors.New(handler.state.ErrorMsg)
	} else {
		handler.state.LastError = nil
	}

	return nil
}

func (handler *Handler) setState() (err error) {
	if handler.state.LastError != nil {
		handler.state.ErrorMsg = handler.state.LastError.Error()
	} else {
		handler.state.ErrorMsg = ""
	}

	jsonState, err := json.Marshal(handler.state)
	if err != nil {
		return err
	}

	if err = handler.storage.SetOperationState(jsonState); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) finishOperation(operationError error) {
	handler.Lock()
	defer handler.Unlock()

	if operationError != nil {
		log.Errorf("Operation %s failed: %s", handler.state.OperationType, operationError)
	} else {
		log.Infof("Operation %s successfully finished", handler.state.OperationType)

		handler.currentVersion = handler.state.OperationVersion
	}

	if err := handler.platform.SetVersion(handler.currentVersion); err != nil {
		log.Errorf("Can't set current version: %s", err)
	}

	handler.state.OperationState = idleState
	handler.state.LastError = operationError

	if err := handler.setState(); err != nil {
		log.Errorf("Can't save update handler state: %s", err)
	}

	status := umprotocol.SuccessStatus

	if handler.state.LastError != nil {
		status = umprotocol.FailedStatus
	}

	handler.statusChannel <- status
}

func (handler *Handler) upgrade() {
	for {
		switch handler.state.OperationState {
		case initState:
			if err := handler.unpackImage(); err != nil {
				handler.switchState(finishState, err)
				break
			}

			handler.switchState(upgradeState, nil)

		case upgradeState:
			if err := handler.upgradeModules(); err != nil {
				handler.switchState(cancelState, err)
				break
			}

			handler.switchState(finishState, nil)

		case cancelState:
			if err := handler.cancelUpgradeModules(); err != nil {
				log.Errorf("Error canceling upgrade modules: %s", err)
			}

			handler.finishOperation(handler.state.LastError)

			return

		case finishState:
			var err error

			if err = handler.finishUpgradeModules(); err != nil {
				log.Errorf("Error finishing upgrade modules: %s", err)
			}

			handler.finishOperation(err)

			return
		}
	}
}

func (handler *Handler) revert() {
	for {
		switch handler.state.OperationState {
		case initState:
			handler.switchState(revertState, nil)

		case revertState:
			if err := handler.revertModules(); err != nil {
				handler.switchState(cancelState, err)
				break
			}

			handler.switchState(finishState, nil)

		case cancelState:
			if err := handler.cancelRevertModules(); err != nil {
				log.Errorf("Error canceling revert modules: %s", err)
			}

			handler.finishOperation(handler.state.LastError)

			return

		case finishState:
			var err error

			if err = handler.finishRevertModules(); err != nil {
				log.Errorf("Error finishing revert modules: %s", err)
			}

			handler.finishOperation(err)

			return
		}
	}
}

func (handler *Handler) switchState(state operationState, lastError error) {
	handler.Lock()
	defer handler.Unlock()

	handler.state.OperationState = state
	handler.state.LastError = lastError

	log.WithFields(log.Fields{"state": handler.state.OperationState, "operation": handler.state.OperationType}).Debugf("State changed")

	if err := handler.setState(); err != nil {
		if handler.state.LastError != nil {
			handler.state.LastError = err
		}

		handler.finishOperation(handler.state.LastError)
	}
}

func (handler *Handler) unpackImage() (err error) {
	if err = os.RemoveAll(handler.bundleDir); err != nil {
		return err
	}

	if err = os.MkdirAll(handler.bundleDir, 0755); err != nil {
		return err
	}

	if err = image.UntarGZArchive(handler.state.ImagePath, handler.bundleDir); err != nil {
		return err
	}

	return nil
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

func (handler *Handler) upgradeModule(id, path string) (err error) {
	log.WithField("id", id).Info("Upgrade module")

	module, ok := handler.modules[id]
	if !ok {
		return errors.New("module not found")
	}

	if _, err = module.Upgrade(handler.state.OperationVersion, path); err != nil {
		log.WithField("id", id).Errorf("Upgrade module failed: %s", err)
		return err
	}

	return nil
}

func (handler *Handler) upgradeModules() (err error) {
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

		status = handler.upgradeModule(item.Type, path.Join(handler.bundleDir, item.Path))

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

func (handler *Handler) cancelUpgradeModules() (err error) {
	return nil
}

func (handler *Handler) finishUpgradeModules() (err error) {
	return nil
}

func (handler *Handler) revertModule(id string) (err error) {
	log.WithField("id", id).Info("Revert module")

	module, ok := handler.modules[id]
	if !ok {
		return errors.New("module not found")
	}
	if _, err = module.Revert(handler.state.OperationVersion); err != nil {
		log.WithField("id", id).Errorf("Revert module failed: %s", err)
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

func (handler *Handler) cancelRevertModules() (err error) {
	return nil
}

func (handler *Handler) finishRevertModules() (err error) {
	return nil
}
