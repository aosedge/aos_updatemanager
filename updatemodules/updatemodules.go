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

package updatemodules

import (
	"fmt"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var moduleMap = map[string]NewFunc{}

/*******************************************************************************
 * Types
 ******************************************************************************/

// NewFunc new function type
type NewFunc func(id string, configJSON []byte) (module interface{}, err error)

/*******************************************************************************
 * Private
 ******************************************************************************/

// Register registers new update module
func Register(name string, newFunc NewFunc) {
	moduleMap[name] = newFunc
}

// New creates new module instance
func New(id, name string, configJSON []byte) (module interface{}, err error) {
	newFunc, ok := moduleMap[name]
	if !ok {
		return nil, fmt.Errorf("update module '%s' not found", name)
	}

	return newFunc(id, configJSON)
}
