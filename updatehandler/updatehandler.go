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
	"gitpct.epam.com/epmd-aepr/aos_common/image"
	"gitpct.epam.com/epmd-aepr/aos_common/umprotocol"

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

//
// Upgrade module state machine:
//
//                      -> upgrade request                     -> initModuleState
// initModuleState      -> upgrade module                      -> upgradingModuleState
// upgradingModuleState -> module upgraded                     -> upgradedModuleState
// upgradedModuleState  -> finish upgrade module               -> finishingModuleState
// finishingModuleState -> module upgrade finished             -> finishedModuleState
//
// Revert module state machine:
//
//                      -> revert request                      ->
// finishedModuleState  -> revert module                       -> revertingModuleState
// revertingModuleState -> module reverted                     -> revertedModuleState
// revertedModuleState  -> finish revert module                -> finishingModuleState
// finishingModuleState -> module revert finished              -> finishedModuleState
//
// If some error happens during upgrade/revert modules and cancel procedure is performed:
//
// upgradedModuleState  -> cancel upgrade module               -> cancelingModuleState
// cancelingModuleState -> module cancel upgrade finished      -> canceledModuleState
//
// The same sequence is applied for revert canceling
//
const (
	initModuleState = iota
	upgradingModuleState
	upgradedModuleState
	revertingModuleState
	revertedModuleState
	cancelingModuleState
	canceledModuleState
	finishingModuleState
	finishedModuleState
)

const bundleDir = "bundleDir"
const metadataFileName = "metadata.json"

const statusChannelSize = 1

/*******************************************************************************
 * Vars
 ******************************************************************************/

var plugins = make(map[string]NewPlugin)

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

	wg sync.WaitGroup

	statusChannel chan umprotocol.StatusRsp
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
}

// PlatformController platform controller
type PlatformController interface {
	GetVersion() (version uint64, err error)
	SetVersion(version uint64) (err error)
	GetPlatformID() (id string, err error)
	SystemReboot() (err error)
}

// NewPlugin plugin new function
type NewPlugin func(id string, configJSON json.RawMessage) (module UpdateModule, err error)

type handlerState struct {
	OperationType    operationType          `json:"operationType"`
	OperationState   operationState         `json:"operationState"`
	OperationVersion uint64                 `json:"operationVersion"`
	ImagePath        string                 `json:"imagePath"`
	ImageMetadata    imageMetadata          `json:"imageMetadata"`
	LastError        error                  `json:"-"`
	ErrorMsg         string                 `json:"errorMsg"`
	ModuleStates     map[string]moduleState `json:"moduleStates"`
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
type moduleState int

type moduleOperation func(module UpdateModule) (rebootRequired bool, err error)

/*******************************************************************************
 * Public
 ******************************************************************************/

// RegisterPlugin registers data adapter plugin
func RegisterPlugin(plugin string, newFunc NewPlugin) {
	log.WithField("plugin", plugin).Info("Register plugin")

	plugins[plugin] = newFunc
}

// New returns pointer to new Handler
func New(cfg *config.Config, platform PlatformController, storage StateStorage) (handler *Handler, err error) {
	handler = &Handler{
		platform:      platform,
		storage:       storage,
		upgradeDir:    cfg.UpgradeDir,
		bundleDir:     path.Join(cfg.UpgradeDir, bundleDir),
		statusChannel: make(chan umprotocol.StatusRsp, statusChannelSize),
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

	for _, moduleCfg := range cfg.Modules {
		if moduleCfg.Disabled {
			log.WithField("id", moduleCfg.ID).Debug("Skip disabled module")
			continue
		}

		module, err := createModule(moduleCfg.Plugin, moduleCfg.ID, moduleCfg.Params)
		if err != nil {
			return nil, err
		}

		handler.modules[moduleCfg.ID] = module
	}

	handler.wg.Add(1)
	go handler.init()

	return handler, nil
}

// GetStatus returns update status
func (handler *Handler) GetStatus() (status umprotocol.StatusRsp) {
	handler.Lock()
	defer handler.Unlock()

	return handler.getStatus()
}

// Upgrade performs upgrade operation
func (handler *Handler) Upgrade(version uint64, imageInfo umprotocol.ImageInfo) (err error) {
	handler.Lock()
	defer handler.Unlock()

	handler.wg.Wait()

	log.WithField("version", version).Info("Upgrade")

	if version <= handler.currentVersion {
		return fmt.Errorf("wrong upgrade version: %d", version)
	}

	if handler.state.OperationState != idleState {
		return errors.New("wrong state")
	}

	imagePath := path.Join(handler.upgradeDir, imageInfo.Path)

	if err = image.CheckFileInfo(imagePath, image.FileInfo{
		Sha256: imageInfo.Sha256,
		Sha512: imageInfo.Sha512,
		Size:   imageInfo.Size}); err != nil {
		return err
	}

	handler.state = handlerState{
		OperationState:   initState,
		OperationVersion: version,
		OperationType:    upgradeOperation,
		ImagePath:        imagePath,
		ModuleStates:     make(map[string]moduleState)}

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

	handler.wg.Wait()

	log.WithField("version", version).Info("Revert")

	if version >= handler.currentVersion {
		return fmt.Errorf("wrong revert version: %d", version)
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
func (handler *Handler) StatusChannel() (statusChannel <-chan umprotocol.StatusRsp) {
	return handler.statusChannel
}

// Close closes update handler
func (handler *Handler) Close() {
	log.Debug("Close update handler")

	for _, module := range handler.modules {
		module.Close()
	}

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

func (state moduleState) String() string {
	return [...]string{
		"init", "upgrading", "upgraded", "reverting", "reverted", "canceling", "canceled", "finishing", "finished"}[state]
}

func createModule(plugin, id string, params json.RawMessage) (module UpdateModule, err error) {
	newFunc, ok := plugins[plugin]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", plugin)
	}

	if module, err = newFunc(id, params); err != nil {
		return nil, err
	}

	return module, nil
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

func (handler *Handler) init() {
	defer handler.wg.Done()

	for id, module := range handler.modules {
		log.Debugf("Initializing module %s", id)

		if err := module.Init(); err != nil {
			log.Errorf("Can't initialize module %s: %s", id, err)
		}
	}

	if handler.state.OperationState != idleState {
		switch {
		case handler.state.OperationType == upgradeOperation:
			go handler.upgrade()

		case handler.state.OperationType == revertOperation:
			go handler.revert()
		}
	}
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

	handler.statusChannel <- handler.getStatus()
}

func (handler *Handler) upgrade() {
	var (
		rebootRequired bool
		err            error
	)

	for {
		switch handler.state.OperationState {
		case initState:
			if err = handler.unpackImage(); err != nil {
				log.Errorf("Can't unpack image: %s", err)

				handler.finishOperation(err)

				return
			}

			handler.switchState(upgradeState, nil)

		case upgradeState:
			rebootRequired, err = handler.moduleOperation(
				upgradingModuleState, upgradedModuleState, []moduleState{upgradedModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					id := module.GetID()

					for _, item := range handler.state.ImageMetadata.UpdateItems {
						if id == item.Type {
							return module.Upgrade(handler.state.OperationVersion, path.Join(handler.bundleDir, item.Path))
						}
					}

					return false, fmt.Errorf("module %s not found", id)
				}, true)
			if err != nil {
				handler.switchState(cancelState, err)
				break
			}

			if rebootRequired {
				break
			}

			handler.switchState(finishState, nil)

		case cancelState:
			rebootRequired, err = handler.moduleOperation(
				cancelingModuleState, canceledModuleState, []moduleState{canceledModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					return module.CancelUpgrade(handler.state.OperationVersion)
				}, false)
			if err != nil {
				log.Errorf("Error canceling upgrade modules: %s", err)
			}

			if rebootRequired {
				break
			}

			handler.finishOperation(handler.state.LastError)

			return

		case finishState:
			if _, err = handler.moduleOperation(finishingModuleState, finishedModuleState, []moduleState{finishedModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					return false, module.FinishUpgrade(handler.state.OperationVersion)
				}, false); err != nil {
				log.Errorf("Error finishing upgrade modules: %s", err)
			}

			handler.finishOperation(err)

			return
		}

		if rebootRequired {
			log.Debug("System reboot is required")

			if err = handler.platform.SystemReboot(); err != nil {
				log.Errorf("Can't perform system reboot: %s", err)
			}

			return
		}
	}
}

func (handler *Handler) revert() {
	var (
		rebootRequired bool
		err            error
	)

	for {
		switch handler.state.OperationState {
		case initState:
			handler.switchState(revertState, nil)

		case revertState:
			rebootRequired, err = handler.moduleOperation(
				revertingModuleState, revertedModuleState, []moduleState{revertedModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					return module.Revert(handler.state.OperationVersion)
				}, true)
			if err != nil {
				handler.switchState(cancelState, err)
				break
			}

			if rebootRequired {
				break
			}

			handler.switchState(finishState, nil)

		case cancelState:
			rebootRequired, err = handler.moduleOperation(
				cancelingModuleState, canceledModuleState, []moduleState{finishedModuleState, canceledModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					return module.CancelRevert(handler.state.OperationVersion)
				}, false)
			if err != nil {
				log.Errorf("Error canceling revert modules: %s", err)
			}

			if rebootRequired {
				break
			}

			handler.finishOperation(handler.state.LastError)

			return

		case finishState:
			var err error

			if _, err = handler.moduleOperation(finishingModuleState, finishedModuleState, []moduleState{finishedModuleState},
				func(module UpdateModule) (rebootRequired bool, err error) {
					return false, module.FinishRevert(handler.state.OperationVersion)
				}, false); err != nil {
				log.Errorf("Error finishing revert modules: %s", err)
			}

			handler.finishOperation(err)

			return
		}

		if rebootRequired {
			log.Debug("System reboot is required")

			if err = handler.platform.SystemReboot(); err != nil {
				log.Errorf("Can't perform system reboot: %s", err)
			}

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
	log.WithFields(log.Fields{"destination": handler.bundleDir, "source": handler.state.ImagePath}).Debug("Unpack image")

	if err = os.RemoveAll(handler.bundleDir); err != nil {
		return err
	}

	if err = os.MkdirAll(handler.bundleDir, 0755); err != nil {
		return err
	}

	if err = image.UntarGZArchive(handler.state.ImagePath, handler.bundleDir); err != nil {
		return err
	}

	if handler.state.ImageMetadata, err = handler.getImageMetadata(); err != nil {
		return err
	}

	platformID, err := handler.platform.GetPlatformID()
	if err != nil {
		return err
	}

	if handler.state.ImageMetadata.PlatformID != platformID {
		return errors.New("wrong platform ID")
	}

	for _, item := range handler.state.ImageMetadata.UpdateItems {
		if _, ok := handler.modules[item.Type]; !ok {
			return fmt.Errorf("module %s not found", item.Type)
		}

		handler.state.ModuleStates[item.Type] = initModuleState
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

func (handler *Handler) moduleOperation(beginState, endState moduleState,
	skipStates []moduleState, operation moduleOperation, stopOnError bool) (rebootRequired bool, operationErr error) {
	for id, state := range handler.state.ModuleStates {
		skip := false

		for _, skipState := range skipStates {
			if state == skipState {
				skip = true
			}
		}

		if skip {
			continue
		}

		module, ok := handler.modules[id]
		if !ok {
			err := fmt.Errorf("module %s not found", id)

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}

			continue
		}

		if err := handler.setModuleState(id, beginState); err != nil {
			log.Errorf("Can't set module state: %s", err)

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}
		}

		moduleRebootRequired, err := operation(module)
		if err != nil {
			log.Errorf("Module operation error: %s", err)

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}
		}

		if moduleRebootRequired {
			log.WithField("id", id).Debug("Module reboot required")

			rebootRequired = true

			continue
		}

		if err := handler.setModuleState(id, endState); err != nil {
			log.Errorf("Can't set module state: %s", err)

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}
		}
	}

	return rebootRequired, operationErr
}

func (handler *Handler) setModuleState(id string, state moduleState) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithFields(log.Fields{"state": state, "id": id}).Debugf("Module state changed")

	handler.state.ModuleStates[id] = state

	if err := handler.setState(); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) getStatus() (status umprotocol.StatusRsp) {
	status.Status = umprotocol.SuccessStatus

	if handler.state.OperationState != idleState {
		status.Status = umprotocol.InProgressStatus
	}

	if handler.state.LastError != nil {
		status.Status = umprotocol.FailedStatus
	}

	if handler.state.OperationType == revertOperation {
		status.Operation = umprotocol.RevertOperation
	}

	if handler.state.OperationType == upgradeOperation {
		status.Operation = umprotocol.UpgradeOperation
	}

	status.CurrentVersion = handler.currentVersion
	status.RequestedVersion = handler.state.OperationVersion
	status.Error = handler.state.ErrorMsg

	return status
}
