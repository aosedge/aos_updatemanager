// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
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

package sshmodule

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/aoscloud/aos_common/aoserrors"
	log "github.com/sirupsen/logrus"
	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"

	"github.com/aoscloud/aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

// Name module name.
const Name = "ssh"

/*******************************************************************************
 * Types
 ******************************************************************************/

// SSHModule SSH module.
type SSHModule struct {
	id string
	sync.Mutex
	config         moduleConfig
	storage        updatehandler.ModuleStorage
	filePath       string
	vendorVersion  string
	pendingVersion string
}

type moduleConfig struct {
	Host     string   `json:"host"`
	User     string   `json:"user"`
	Password string   `json:"password"`
	DestPath string   `json:"destPath"`
	Commands []string `json:"commands"`
}

type moduleState struct {
	Version string `json:"version"`
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates ssh module instance.
func New(id string, configJSON json.RawMessage,
	storage updatehandler.ModuleStorage,
) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Debug("Create SSH module")

	sshModule := &SSHModule{id: id, storage: storage}

	if configJSON != nil {
		if err = json.Unmarshal(configJSON, &sshModule.config); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	stateJSON, err := storage.GetModuleState(id)
	if err != nil {
		stateJSON = []byte{}
	}

	state := moduleState{Version: "0.0.0"}

	if len(stateJSON) != 0 {
		if err = json.Unmarshal(stateJSON, &state); err != nil {
			return nil, aoserrors.Wrap(err)
		}
	}

	sshModule.vendorVersion = state.Version

	return sshModule, nil
}

// Close closes ssh module.
func (module *SSHModule) Close() (err error) {
	log.WithField("id", module.id).Debug("Close SSH module")
	return nil
}

// Init initializes module.
func (module *SSHModule) Init() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Init test module")

	return nil
}

// Prepare prepares module update.
func (module *SSHModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	log.WithFields(log.Fields{
		"id":        module.id,
		"imagePath": imagePath,
	}).Debug("Prepare SSH module")

	module.filePath = imagePath

	module.pendingVersion = vendorVersion

	return nil
}

// GetID returns module ID.
func (module *SSHModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// GetVendorVersion returns vendor version.
func (module *SSHModule) GetVendorVersion() (version string, err error) {
	module.Lock()
	defer module.Unlock()

	return module.vendorVersion, nil
}

// Update performs module update.
func (module *SSHModule) Update() (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"id": module.id}).Debug("Update SSH module")

	// Create SSH connection
	config := &ssh.ClientConfig{
		User:            module.config.User,
		Auth:            []ssh.AuthMethod{ssh.Password(module.config.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // use as example update module
	}

	client, err := ssh.Dial("tcp", module.config.Host, config)
	if err != nil {
		return false, aoserrors.Wrap(err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return false, aoserrors.Wrap(err)
	}
	defer session.Close()

	log.WithFields(log.Fields{"src": module.filePath, "dst": module.config.DestPath}).Debug("Copy file")

	// Copy file to the remote DestDir
	if err = scp.CopyPath(module.filePath, module.config.DestPath, session); err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.runCommands(client); err != nil {
		return false, aoserrors.Wrap(err)
	}

	module.vendorVersion = module.pendingVersion

	return false, nil
}

// Apply applies current update.
func (module *SSHModule) Apply() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Apply SSH module")

	state := moduleState{Version: module.pendingVersion}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return false, aoserrors.Wrap(err)
	}

	if err = module.storage.SetModuleState(module.id, stateJSON); err != nil {
		return false, aoserrors.Wrap(err)
	}

	return false, nil
}

// Revert reverts current update.
func (module *SSHModule) Revert() (rebootRequired bool, err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Revert SSH module")

	return false, nil
}

// Reboot performs module reboot.
func (module *SSHModule) Reboot() (err error) {
	log.WithFields(log.Fields{"id": module.id}).Debug("Reboot SSH module")

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (module *SSHModule) runCommands(client *ssh.Client) (err error) {
	session, err := client.NewSession()
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err = session.Shell(); err != nil {
		return aoserrors.Wrap(err)
	}

	for _, command := range module.config.Commands {
		log.WithField("command", command).Debug("SSH command")

		if _, err = fmt.Fprintf(stdin, "%s\n", command); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	if _, err = fmt.Fprintf(stdin, "%s\n", "exit"); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = session.Wait(); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
