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
	"sync"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/image"
	"gitpct.epam.com/epmd-aepr/aos_common/umprotocol"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

//
// Update state machine:
//
// stateIdle          -> update request                         -> stateUpdate
// stateUpdate        -> update components                      -> stateFinish
// stateFinish        -> send status                            -> stateIdle
//
// If some error happens during update components:
//
// stateUpdate        -> update components fail                 -> stateCancel
// stateCancel        -> cancel update components, send status  -> stateIdle
//

const (
	stateIdle = iota
	stateUpdate
	stateCancel
	stateFinish
)

//
// Update component state machine:
//
//                         -> update request                      -> stateComponentInit
// stateComponentInit      -> update component                    -> stateComponentUpdated
// stateComponentUpdated   -> finish component                    -> stateComponentFinished
//
// If some error happens during update component, cancel procedure is performed:
//
// stateComponentUpdated   -> cancel component                    -> stateComponentCanceled
//
const (
	stateComponentInit = iota
	stateComponentUpdated
	stateComponentCanceled
	stateComponentFinished
)

const statusChannelSize = 1

/*******************************************************************************
 * Vars
 ******************************************************************************/

var plugins = make(map[string]NewPlugin)

var newPlatformController NewPlatfromContoller

/*******************************************************************************
 * Types
 ******************************************************************************/

// Handler update handler
type Handler struct {
	sync.Mutex

	platform PlatformController
	storage  StateStorage

	components map[string]UpdateModule

	state handlerState

	wg sync.WaitGroup

	statusChannel chan []umprotocol.ComponentStatus
}

// UpdateModule interface for module plugin
type UpdateModule interface {
	// GetID returns module ID
	GetID() (id string)
	// GetVendorVersion returns vendor version
	GetVendorVersion() (version string, err error)
	// Init initializes module
	Init() (err error)
	// Update updates module
	Update(imagePath string, vendorVersion string, annotations json.RawMessage) (rebootRequired bool, err error)
	// Cancel cancels update
	Cancel() (rebootRequired bool, err error)
	// Finish finished update
	Finish() (err error)
	// Close closes update module
	Close() (err error)
}

// StateStorage provides API to store/retreive persistent data
type StateStorage interface {
	SetUpdateState(jsonState []byte) (err error)
	GetUpdateState() (jsonState []byte, err error)
	GetModuleState(id string) (state []byte, err error)
	SetModuleState(id string, state []byte) (err error)
	GetControllerState(controllerID string, name string) (value []byte, err error)
	SetControllerState(controllerID string, name string, value []byte) (err error)
}

// PlatformController platform controller
type PlatformController interface {
	SystemReboot() (err error)
	Close() (err error)
}

// NewPlugin update module new function
type NewPlugin func(id string, configJSON json.RawMessage, controller PlatformController, storage StateStorage) (module UpdateModule, err error)

// NewPlatfromContoller plugin for platform Contoller
type NewPlatfromContoller func(storage StateStorage, modules []config.ModuleConfig) (controller PlatformController, err error)

type componentState struct {
	UpdateState   updateComponentState `json:"updateState"`
	AosVersion    uint64               `json:"aosVersion"`
	VendorVersion string               `json:"vendorVersion"`
	Path          string               `json:"path"`
	Annotations   json.RawMessage      `json:"annotations"`
	Error         string               `json:"error,omitempty"`
}

type handlerState struct {
	UpdateState     updateState                `json:"updateState"`
	ComponentStates map[string]*componentState `json:"componentStates"`
	AosVersions     map[string]uint64          `json:"aosVersions"`
}

type updateState int
type updateComponentState int

type componentOperation func(component UpdateModule,
	state *componentState) (rebootRequired bool, err error)

/*******************************************************************************
 * Public
 ******************************************************************************/

// RegisterPlugin registers update plugin
func RegisterPlugin(plugin string, newFunc NewPlugin) {
	log.WithField("plugin", plugin).Info("Register update plugin")

	plugins[plugin] = newFunc
}

//RegisterControllerPlugin  registers platfrom controller plugin
func RegisterControllerPlugin(newFunc NewPlatfromContoller) {
	newPlatformController = newFunc
}

// New returns pointer to new Handler
func New(cfg *config.Config, storage StateStorage) (handler *Handler, err error) {
	log.Debug("Create update handler")

	handler = &Handler{
		storage:       storage,
		statusChannel: make(chan []umprotocol.ComponentStatus, statusChannelSize),
	}

	if newPlatformController == nil {
		return nil, errors.New("controller plugin should be registered")
	}

	handler.platform, err = newPlatformController(storage, cfg.UpdateModules)
	if err != nil {
		return nil, err
	}

	if err = handler.getState(); err != nil {
		return nil, err
	}

	if handler.state.AosVersions == nil {
		handler.state.AosVersions = make(map[string]uint64)
	}

	handler.components = make(map[string]UpdateModule)

	for _, moduleCfg := range cfg.UpdateModules {
		if moduleCfg.Disabled {
			log.WithField("id", moduleCfg.ID).Debug("Skip disabled module")
			continue
		}

		component, err := handler.createComponent(moduleCfg.Plugin, moduleCfg.ID, moduleCfg.Params)
		if err != nil {
			return nil, err
		}

		handler.components[moduleCfg.ID] = component
	}

	handler.wg.Add(1)
	go handler.init()

	return handler, nil
}

// GetStatus returns update status
func (handler *Handler) GetStatus() (status []umprotocol.ComponentStatus) {
	handler.Lock()
	defer handler.Unlock()

	return handler.getStatus()
}

// Update performs components updates
func (handler *Handler) Update(infos []umprotocol.ComponentInfo) {
	handler.Lock()
	defer handler.Unlock()

	if handler.state.UpdateState != stateIdle {
		log.Warn("Another update is in progress")
	}

	handler.state.ComponentStates = make(map[string]*componentState)

	for _, info := range infos {
		log.WithFields(log.Fields{
			"ID":            info.ID,
			"AosVersion":    info.AosVersion,
			"VendorVersion": info.VendorVersion,
			"Path":          info.Path}).Debug("Update component")

		handler.state.ComponentStates[info.ID] = &componentState{
			UpdateState:   stateComponentInit,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Annotations:   info.Annotations,
			Path:          info.Path,
		}

		component, ok := handler.components[info.ID]
		if !ok {
			err := errors.New("component not found")

			handler.state.ComponentStates[info.ID].Error = err.Error()

			log.WithField("id", info.ID).Errorf("Component update error: %s", err)

			continue
		}

		vendorVersion, err := component.GetVendorVersion()
		if err == nil && info.VendorVersion != "" {
			if vendorVersion == info.VendorVersion {
				log.WithField("id", info.ID).Warnf("Component already has required vendor version: %s", vendorVersion)

				delete(handler.state.ComponentStates, info.ID)

				continue
			}
		}

		if err := image.CheckFileInfo(info.Path, image.FileInfo{
			Sha256: info.Sha256,
			Sha512: info.Sha512,
			Size:   info.Size}); err != nil {

			handler.state.ComponentStates[info.ID].Error = err.Error()

			log.WithField("id", info.ID).Errorf("Component update error: %s", err)

			continue
		}
	}

	for _, state := range handler.state.ComponentStates {
		if state.Error != "" {
			handler.finishUpdate()

			return
		}
	}

	handler.state.UpdateState = stateUpdate

	log.WithField("state", handler.state.UpdateState).Debugf("State changed")

	if err := handler.setState(); err != nil {
		log.Errorf("Can't set update handler state: %s", err)
	}

	handler.statusChannel <- handler.getStatus()

	go handler.update()
}

// StatusChannel this channel is used to notify about component statuses
func (handler *Handler) StatusChannel() (statusChannel <-chan []umprotocol.ComponentStatus) {
	return handler.statusChannel
}

// Close closes update handler
func (handler *Handler) Close() {
	log.Debug("Close update handler")

	for _, component := range handler.components {
		component.Close()
	}

	handler.platform.Close()

	close(handler.statusChannel)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state updateState) String() string {
	return [...]string{
		"idle", "update", "cancel", "finish"}[state]
}

func (state updateComponentState) String() string {
	return [...]string{
		"init", "updated", "canceled", "finished"}[state]
}

func (handler *Handler) createComponent(plugin, id string, params json.RawMessage) (module UpdateModule, err error) {
	newFunc, ok := plugins[plugin]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", plugin)
	}

	if module, err = newFunc(id, params, handler.platform, handler.storage); err != nil {
		return nil, err
	}

	return module, nil
}

func (handler *Handler) getState() (err error) {
	jsonState, err := handler.storage.GetUpdateState()
	if err != nil {
		return err
	}

	if len(jsonState) == 0 {
		return nil
	}

	if err = json.Unmarshal(jsonState, &handler.state); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) setState() (err error) {
	jsonState, err := json.Marshal(handler.state)
	if err != nil {
		return err
	}

	if err = handler.storage.SetUpdateState(jsonState); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) init() {
	defer handler.wg.Done()

	for id, module := range handler.components {
		log.Debugf("Initializing module %s", id)

		if err := module.Init(); err != nil {
			log.Errorf("Can't initialize module %s: %s", id, err)
		}
	}

	if handler.state.UpdateState != stateIdle {
		go handler.update()
	}
}

func (handler *Handler) finishUpdate() {
	for id, componentState := range handler.state.ComponentStates {
		if componentState.Error != "" {
			continue
		}

		if componentState.UpdateState == stateComponentFinished {
			handler.state.AosVersions[id] = componentState.AosVersion
		}

		delete(handler.state.ComponentStates, id)
	}

	handler.state.UpdateState = stateIdle

	log.WithField("state", handler.state.UpdateState).Debugf("State changed")

	if err := handler.setState(); err != nil {
		log.Errorf("Can't set update handler state: %s", err)
	}

	handler.statusChannel <- handler.getStatus()
}

func (handler *Handler) update() {
	var (
		rebootRequired bool
		err            error
	)

	handler.wg.Wait()

	for {
		switch handler.state.UpdateState {
		case stateUpdate:
			rebootRequired, err = handler.componentOperation(stateComponentUpdated,
				func(component UpdateModule, state *componentState) (rebootRequired bool, err error) {
					return component.Update(state.Path, state.VendorVersion, state.Annotations)
				}, true)
			if err != nil {
				handler.switchState(stateCancel)

				break
			}

			if rebootRequired {
				break
			}

			handler.switchState(stateFinish)

		case stateCancel:
			rebootRequired, _ = handler.componentOperation(stateComponentCanceled,
				func(component UpdateModule, state *componentState) (rebootRequired bool, err error) {
					return component.Cancel()
				}, false)

			if rebootRequired {
				break
			}

			handler.Lock()
			handler.finishUpdate()
			handler.Unlock()

			return

		case stateFinish:
			handler.componentOperation(stateComponentFinished,
				func(component UpdateModule, state *componentState) (rebootRequired bool, err error) {
					return false, component.Finish()
				}, false)

			handler.Lock()
			handler.finishUpdate()
			handler.Unlock()

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

func (handler *Handler) switchState(state updateState) {
	handler.Lock()
	defer handler.Unlock()

	handler.state.UpdateState = state

	log.WithField("state", handler.state.UpdateState).Debugf("State changed")

	if err := handler.setState(); err != nil {
		log.Errorf("Can't set update handler state: %s", err)
	}
}

func (handler *Handler) componentOperation(newState updateComponentState,
	operation componentOperation, stopOnError bool) (rebootRequired bool, operationErr error) {
	for id, componentState := range handler.state.ComponentStates {
		if componentState.UpdateState == newState {
			continue
		}

		component, ok := handler.components[id]
		if !ok {
			err := fmt.Errorf("component %s not found", id)

			log.WithField("id", id).Errorf("Component operation error: %s", err)

			if componentState.Error == "" {
				componentState.Error = err.Error()
			}

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}

			continue
		}

		moduleRebootRequired, err := operation(component, componentState)
		if err != nil {
			log.WithField("id", id).Errorf("Component operation error: %s", err)

			if componentState.Error == "" {
				componentState.Error = err.Error()
			}

			if operationErr == nil {
				operationErr = err
			}

			if stopOnError {
				return false, operationErr
			}
		}

		if moduleRebootRequired {
			log.WithField("id", id).Debug("Component reboot required")

			rebootRequired = true

			continue
		}

		if err := handler.setComponentState(id, newState); err != nil {
			log.WithField("id", id).Errorf("Can't set module state: %s", err)

			if componentState.Error == "" {
				componentState.Error = err.Error()
			}

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

func (handler *Handler) setComponentState(id string, state updateComponentState) (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.WithFields(log.Fields{"state": state, "id": id}).Debugf("Component state changed")

	handler.state.ComponentStates[id].UpdateState = state

	if err := handler.setState(); err != nil {
		return err
	}

	return nil
}

func (handler *Handler) getStatus() (status []umprotocol.ComponentStatus) {
	for id, component := range handler.components {
		componentStatus := umprotocol.ComponentStatus{ID: id, Status: umprotocol.StatusInstalled}

		vendorVersion, err := component.GetVendorVersion()
		if err == nil {
			componentStatus.VendorVersion = vendorVersion
		}

		aosVersion, ok := handler.state.AosVersions[id]
		if ok {
			componentStatus.AosVersion = aosVersion
		}

		status = append(status, componentStatus)

		componentState, ok := handler.state.ComponentStates[id]
		if ok {
			componentStatus := umprotocol.ComponentStatus{
				ID:            id,
				AosVersion:    componentState.AosVersion,
				VendorVersion: componentState.VendorVersion,
				Status:        umprotocol.StatusInstalling,
			}

			if componentState.Error != "" {
				componentStatus.Status = umprotocol.StatusError
				componentStatus.Error = componentState.Error
			}

			status = append(status, componentStatus)
		}
	}

	return status
}
