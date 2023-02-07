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

package ubootdualparts

import (
	"encoding/json"

	"github.com/aoscloud/aos_common/aoserrors"

	"github.com/aoscloud/aos_updatemanager/updatehandler"
	"github.com/aoscloud/aos_updatemanager/updatemodules/partitions/controllers/ubootcontroller"
	"github.com/aoscloud/aos_updatemanager/updatemodules/partitions/modules/dualpartmodule"
	"github.com/aoscloud/aos_updatemanager/updatemodules/partitions/rebooters/xenstorerebooter"
	"github.com/aoscloud/aos_updatemanager/updatemodules/partitions/updatechecker/systemdchecker"
	"github.com/aoscloud/aos_updatemanager/updatemodules/partitions/utils/bootparams"
)

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

type controllerConfig struct {
	Device      string `json:"device"`
	EnvFileName string `json:"envfilename"`
}

type moduleConfig struct {
	Controller     controllerConfig      `json:"controller"`
	DetectMode     string                `json:"detectMode"`
	Partitions     []string              `json:"partitions"`
	VersionFile    string                `json:"versionFile"`
	SystemdChecker systemdchecker.Config `json:"systemdChecker"`
}

/***********************************************************************************************************************
 * Init
 **********************************************************************************************************************/

func init() {
	updatehandler.RegisterPlugin("ubootdualpart",
		func(id string, configJSON json.RawMessage,
			storage updatehandler.ModuleStorage,
		) (module updatehandler.UpdateModule, err error) {
			var config moduleConfig

			if err = json.Unmarshal(configJSON, &config); err != nil {
				return nil, aoserrors.Wrap(err)
			}

			partitions := config.Partitions
			envDevice := config.Controller.Device

			if config.DetectMode != "" {
				parser, err := bootparams.New()
				if err != nil {
					return nil, aoserrors.Wrap(err)
				}

				if envDevice, err = parser.GetEnvPart(config.DetectMode, config.Controller.Device); err != nil {
					return nil, aoserrors.Wrap(err)
				}

				if partitions, err = parser.GetBootParts(config.DetectMode, config.Partitions); err != nil {
					return nil, aoserrors.Wrap(err)
				}
			}

			controller, err := ubootcontroller.New(envDevice, config.Controller.EnvFileName)
			if err != nil {
				return nil, aoserrors.Wrap(err)
			}

			if module, err = dualpartmodule.New(id, partitions, config.VersionFile,
				controller, storage, &xenstorerebooter.XenstoreRebooter{},
				systemdchecker.New(config.SystemdChecker)); err != nil {
				return nil, aoserrors.Wrap(err)
			}

			return module, nil
		},
	)
}
