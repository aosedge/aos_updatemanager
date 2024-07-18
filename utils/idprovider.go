// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
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

package idprovider

import (
	"os"
	"strings"

	"github.com/aosedge/aos_common/aoserrors"
)

/*******************************************************************************
 * Constants
 ******************************************************************************/

const machineIDFile = "/etc/machine-id"

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// CreateID creates a new ID with the given prefix.
func CreateID(prefix string) (string, error) {
	id, err := getMachineID()
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	return prefix + "_" + id, nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func getMachineID() (string, error) {
	if _, err := os.Stat(machineIDFile); err == nil {
		id, err := os.ReadFile(machineIDFile)
		if err != nil {
			return "", aoserrors.Wrap(err)
		}

		return strings.TrimSpace(string(id)), nil
	}

	return "", aoserrors.Errorf("%s file not found", machineIDFile)
}
