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

package modulemanager

import (
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var moduleMap = map[string]NewFunc{}

/*******************************************************************************
 * Types
 ******************************************************************************/

// ModuleManager module manager instance
type ModuleManager struct {
	modules map[string]interface{}
}

// NewFunc new function type
type NewFunc func(id string, configJSON []byte) (module interface{}, err error)

type closeInterface interface {
	// Close closes module
	Close() (err error)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

// Register registers new update module
func Register(name string, newFunc NewFunc) {
	moduleMap[name] = newFunc
}

// New creates new module instance
func New(cfg *config.Config) (manager *ModuleManager, err error) {
	manager = &ModuleManager{modules: make(map[string]interface{})}

	for _, moduleCfg := range cfg.Modules {
		if moduleCfg.Disabled {
			log.WithField("id", moduleCfg.ID).Debug("Skip disabled module")
			continue
		}

		if _, ok := manager.modules[moduleCfg.ID]; ok {
			log.WithField("id", moduleCfg.ID).Warning("Module already exists")
		}

		newFunc, ok := moduleMap[moduleCfg.Module]
		if !ok {
			return nil, fmt.Errorf("module %s not found", moduleCfg.Module)
		}

		module, err := newFunc(moduleCfg.ID, moduleCfg.Params)
		if err != nil {
			return nil, err
		}

		manager.modules[moduleCfg.ID] = module
	}

	if len(manager.modules) == 0 {
		return nil, errors.New("no valid modules info provided")
	}

	return manager, nil
}

// Close closes module manager
func (manager *ModuleManager) Close() (err error) {
	var retErr error

	for id, module := range manager.modules {
		if closeModule, ok := module.(closeInterface); ok {
			if err = closeModule.Close(); err != nil {
				log.Errorf("error closing module: %s", id)

				if retErr == nil {
					retErr = err
				}
			}
		}
	}

	return retErr
}

// GetModuleByID returns update module by ID
func (manager *ModuleManager) GetModuleByID(id string) (module interface{}, err error) {
	var ok bool

	if module, ok = manager.modules[id]; !ok {
		return nil, fmt.Errorf("module %s not found", id)
	}

	return module, nil
}
