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

package ubootdualparts

import (
	"encoding/json"
	"os"

	"github.com/joelnb/xenstore-go"

	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/partitions/controllers/ubootcontroller"
	"aos_updatemanager/updatemodules/partitions/modules/dualpartmodule"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const xenBus = "/dev/xen/xenbus"
const featureReboot = "control/user-reboot"
const requestReboot = "2"

const exitStatus = 1

/*******************************************************************************
 * Vars
 ******************************************************************************/

type rebootController struct {
}

type controllerConfig struct {
	Device      string `json:"device"`
	EnvFileName string `json:"envfilename"`
}

type moduleConfig struct {
	Controller  controllerConfig `json:"controller"`
	Partitions  []string         `json:"partitions"`
	VersionFile string           `json:"versionFile"`
}

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	updatehandler.RegisterPlugin("ubootdualpart",
		func(id string, configJSON json.RawMessage,
			storage updatehandler.ModuleStorage) (module updatehandler.UpdateModule, err error) {

			var config moduleConfig

			if err = json.Unmarshal(configJSON, &config); err != nil {
				return nil, err
			}

			controller, err := ubootcontroller.New(config.Controller.Device, config.Controller.EnvFileName)
			if err != nil {
				return nil, err
			}

			return dualpartmodule.New(id, config.Partitions, config.VersionFile,
				controller, storage, &rebootController{})
		},
	)
}

/*******************************************************************************
 * Interfaces
 ******************************************************************************/

func (reboot *rebootController) Reboot() (err error) {
	xenClient, err := xenstore.NewXenBusClient(xenBus)
	if err != nil {
		return err
	}

	_, err = xenClient.Write(featureReboot, requestReboot)

	// Xen Client should be closed in any case prior to error check
	xenClient.Close()

	if err != nil {
		return err
	}

	// Exit from program. Do not run defers.
	os.Exit(exitStatus)

	return nil
}
