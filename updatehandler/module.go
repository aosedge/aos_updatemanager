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
	"errors"
	"plugin"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Module interface for module plugin
type Module interface {
	// Close closes module
	Close()
	// GetID returns module ID
	GetID() (id string)
	// Upgrade upgrade module
	Upgrade(fileName string) (err error)
	// Revert revert module
	Revert() (err error)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

// newModule creates new module instance
func newModule(id, pluginPath string, configJSON []byte) (module Module, err error) {
	plugin, err := plugin.Open(pluginPath)
	if err != nil {
		return module, err
	}

	newModuleSymbol, err := plugin.Lookup("NewModule")
	if err != nil {
		return module, err
	}

	newModuleFunction, ok := newModuleSymbol.(func(id string, configJSON []byte) (Module, error))
	if !ok {
		return module, errors.New("unexpected function type")
	}

	return newModuleFunction(id, configJSON)
}
