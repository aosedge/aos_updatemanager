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

package fs

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"gitpct.epam.com/epmd-aepr/aos_common/aoserrors"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	retryCount = 3
	retryDelay = 1 * time.Second
)

/*******************************************************************************
 * Types
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// Mount creates mount point and mount device to it
func Mount(device string, mountPoint string, fsType string, flags uintptr, opts string) (err error) {
	log.WithFields(log.Fields{"device": device, "type": fsType, "mountPoint": mountPoint}).Debug("Mount partition")

	if err = os.MkdirAll(mountPoint, 0755); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = retry(
		func() error {
			return aoserrors.Wrap(syscall.Mount(device, mountPoint, fsType, flags, opts))
		},
		func(err error) {
			log.Warningf("Mount error: %s, try remount...", err)

			// Try to sync and force umount
			syscall.Unmount(mountPoint, syscall.MNT_FORCE)
		}); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// Umount umount mount point and remove it
func Umount(mountPoint string) (err error) {
	log.WithFields(log.Fields{"mountPoint": mountPoint}).Debug("Umount partition")

	defer func() {
		if removeErr := os.RemoveAll(mountPoint); removeErr != nil {
			log.Errorf("Can't remove mount point: %s", removeErr)
			if err == nil {
				err = aoserrors.Wrap(removeErr)
			}
		}
	}()

	if err = retry(
		func() error {
			syscall.Sync()

			return aoserrors.Wrap(syscall.Unmount(mountPoint, 0))
		},
		func(err error) {
			log.Warningf("Umount error: %s, retry...", err)

			time.Sleep(retryDelay)

			// Try to sync and force umount
			syscall.Sync()
		}); err != nil {

		log.Warningf("Can't umount for: %s", mountPoint)

		if output, err := exec.Command("lsof", mountPoint).CombinedOutput(); err == nil {
			log.Debugf("lsof says: %s", string(output))
		}

		if output, err := exec.Command("fuser", "-cu", mountPoint).CombinedOutput(); err == nil {
			log.Debugf("fuser says: %s", string(output))
		}

		return aoserrors.Wrap(err)
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func retry(caller func() error, restorer func(error)) (err error) {
	i := 0

	for {
		if err = caller(); err == nil {
			return nil
		}

		if i >= retryCount {
			return aoserrors.Wrap(err)
		}

		if restorer != nil {
			restorer(err)
		}

		i++
	}
}
