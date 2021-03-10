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

package rebootcontroller

import (
	"github.com/coreos/go-systemd/v22/dbus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// RebootController reboot constroller instance
type RebootController struct {
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// Reboot reboots the system
func (controller *RebootController) Reboot() (err error) {
	systemd, err := dbus.NewSystemConnection()
	if err != nil {
		return err
	}
	defer systemd.Close()

	channel := make(chan string)

	if _, err = systemd.StartUnit("reboot.target", "replace-irreversibly", nil); err != nil {
		return err
	}

	<-channel

	return nil
}
