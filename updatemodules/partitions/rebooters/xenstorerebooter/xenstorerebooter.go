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

package xenstorerebooter

import (
	"os"

	"github.com/joelnb/xenstore-go"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const xenBus = "/dev/xen/xenbus"
const featureReboot = "control/user-reboot"
const requestReboot = "2"

/*******************************************************************************
 * Types
 ******************************************************************************/

// XenstoreRebooter reboot system using xenstore entry
type XenstoreRebooter struct {
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// Reboot requests to reboot system by writing xenstore entry
func (reboot *XenstoreRebooter) Reboot() (err error) {
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

	os.Exit(0)

	return nil
}