// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2020 Renesas Inc.
// Copyright 2020 EPAM Systems Inc.
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

package boardconfigmodule

import (
	"encoding/json"
	"sync"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// BoardCfgModule board configuration update module
type BoardCfgModule struct {
	id string
	sync.Mutex
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates boardconfig module instance
func New(id string, configJSON json.RawMessage,
	storage updatehandler.StateStorage) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Info("Create boardconfig module")

	boardModule := &BoardCfgModule{id: id}

	return boardModule, nil
}

// Close closes boardconfig module
func (module *BoardCfgModule) Close() (err error) {
	log.WithField("id", module.id).Info("Close boardconfig module")
	return nil
}

// Init initializes module
func (module *BoardCfgModule) Init() (err error) {
	return nil
}

// GetID returns module ID
func (module *BoardCfgModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// GetVendorVersion returns vendor version
func (module *BoardCfgModule) GetVendorVersion() (version string, err error) {
	return "", nil
}

// Update updates module
func (module *BoardCfgModule) Update(imagePath string, vendorVersion string, annotations json.RawMessage) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": imagePath}).Info("Update")

	return false, nil
}

// Cancel cancels update
func (module *BoardCfgModule) Cancel() (rebootRequired bool, err error) {
	return false, nil
}

// Finish finished update
func (module *BoardCfgModule) Finish() (err error) {
	return nil
}
