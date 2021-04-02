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

package overlayxenstore

import (
	"encoding/json"

	"aos_updatemanager/updatehandler"
	"aos_updatemanager/updatemodules/partitions/modules/overlaymodule"
	"aos_updatemanager/updatemodules/partitions/rebooters/xenstorerebooter"
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	updatehandler.RegisterPlugin("overlayxenstore",
		func(id string, configJSON json.RawMessage,
			storage updatehandler.ModuleStorage) (module updatehandler.UpdateModule, err error) {
			return overlaymodule.New(id, configJSON, storage, &xenstorerebooter.XenstoreRebooter{})
		})
}
