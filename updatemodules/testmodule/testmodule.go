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

package testmodule

import (
	"encoding/json"
	"errors"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

// Name module name
const Name = "test"

/*******************************************************************************
 * Types
 ******************************************************************************/

// TestModule test module
type TestModule struct {
	id string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates test module instance
func New(id string, configJSON json.RawMessage,
	storage updatehandler.ModuleStorage) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Debug("Create test module")

	testModule := &TestModule{id: id}

	return testModule, nil
}

// Close closes test module
func (module *TestModule) Close() (err error) {
	log.WithField("id", module.id).Debug("Close test module")
	return nil
}

// Init initializes module
func (module *TestModule) Init() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Init test module")

	return nil
}

// GetID returns module ID
func (module *TestModule) GetID() (id string) {
	return module.id
}

// GetVendorVersion returns vendor version
func (module *TestModule) GetVendorVersion() (version string, err error) {
	return "", errors.New("not supported")
}

// Prepare prepares module update
func (module *TestModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	log.WithFields(log.Fields{
		"id":        module.id,
		"imagePath": imagePath}).Debug("Prepare test module")

	return nil
}

// Update performs module update
func (module *TestModule) Update() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Update test module")

	return false, nil
}

// Apply applies current update
func (module *TestModule) Apply() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Apply test module")

	return false, nil
}

// Revert reverts current update
func (module *TestModule) Revert() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Revert test module")

	return false, nil
}

// Reboot performs module reboot
func (module *TestModule) Reboot() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Reboot test module")

	return nil
}
