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
	"sync"

	log "github.com/sirupsen/logrus"
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
	sync.Mutex
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates test module instance
func New(id string, configJSON []byte) (module *TestModule, err error) {
	log.WithField("id", id).Info("Create test module")

	testModule := &TestModule{id: id}

	return testModule, nil
}

// Close closes test module
func (module *TestModule) Close() (err error) {
	log.WithField("id", module.id).Info("Close test module")
	return nil
}

// Init initializes module
func (module *TestModule) Init() (err error) {
	return nil
}

// GetID returns module ID
func (module *TestModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Upgrade upgrades module
func (module *TestModule) Upgrade(version uint64, fileName string) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": fileName}).Info("Upgrade")

	return false, nil
}

// CancelUpgrade cancels upgrade
func (module *TestModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

// FinishUpgrade finishes upgrade
func (module *TestModule) FinishUpgrade(version uint64) (err error) {
	return nil
}

// Revert revert module
func (module *TestModule) Revert(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithField("id", module.id).Info("Revert")

	return false, nil
}

// CancelRevert cancels revert
func (module *TestModule) CancelRevert(version uint64) (rebootRequired bool, err error) {
	return false, nil
}

// FinishRevert finishes revert
func (module *TestModule) FinishRevert(version uint64) (err error) {
	return nil
}
