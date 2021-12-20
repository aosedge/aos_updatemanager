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

package systemdrebooter

import (
	"os"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/coreos/go-systemd/v22/dbus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// SystemdRebooter reboot system using systemd
type SystemdRebooter struct {
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// Reboot reboots the system
func (controller *SystemdRebooter) Reboot() (err error) {
	systemd, err := dbus.NewSystemConnection()
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer systemd.Close()

	channel := make(chan string)

	if _, err = systemd.StartUnit("reboot.target", "replace-irreversibly", nil); err != nil {
		return aoserrors.Wrap(err)
	}

	<-channel

	os.Exit(0)

	return nil
}
