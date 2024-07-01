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

package ubootcontroller

import (
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/partition"
	"github.com/aosedge/aos_common/utils/fs"
	log "github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"
)

/*******************************************************************************
 * Constants
 ******************************************************************************/

const (
	envMountPoint   = "/tmp/aos/env"
	envMountTimeout = 10 * time.Minute
)

const (
	aosBoot1Ok  = "aos_boot1_ok"
	aosBoot2Ok  = "aos_boot2_ok"
	aosBootPart = "aos_boot_part"
	aosMainPart = "aos_boot_main"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// UbootController instance.
type UbootController struct {
	sync.Mutex

	envDevice   string
	envFsType   string
	envFileName string
	mountTimer  *time.Timer
	mountedPart string

	cfg *ini.File
}

/*******************************************************************************
 * Variables
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new instance of UbootController.
func New(envDevice string, envFileName string) (controller *UbootController, err error) {
	log.Debug("Create Uboot controller")

	if _, err := os.Stat(envDevice); os.IsNotExist(err) {
		log.Errorf("Can't create Uboot controller, wrong device %s", envDevice)
		return nil, aoserrors.Wrap(err)
	}

	info, err := partition.GetPartInfo(envDevice)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	controller = &UbootController{envDevice: envDevice, envFsType: info.FSType, envFileName: envFileName}

	// Unset PrettyFormat to avoid alignment
	ini.PrettyFormat = false

	return controller, nil
}

// Close closes UbootController.
func (controller *UbootController) Close() {
	log.Debug("Close Uboot controller")

	if err := controller.umountEnv(); err != nil {
		log.Errorf("Error closing Uboot controller: %s", err)
	}
}

// GetCurrentBoot returns current boot part index.
func (controller *UbootController) GetCurrentBoot() (index int, err error) {
	return controller.getVar(aosBootPart)
}

// GetMainBoot returns boot main part index.
func (controller *UbootController) GetMainBoot() (index int, err error) {
	if index, err = controller.getVar(aosMainPart); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return index, nil
}

// SetMainBoot sets boot main part index.
func (controller *UbootController) SetMainBoot(index int) (err error) {
	if err = controller.setVar(aosMainPart, strconv.Itoa(index)); err != nil {
		return aoserrors.Wrap(err)
	}

	return aoserrors.Wrap(controller.saveEnv())
}

// SetBootOK sets boot successful flag.
func (controller *UbootController) SetBootOK() (err error) {
	if err = controller.setVar(aosBoot1Ok, "1"); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = controller.setVar(aosBoot2Ok, "1"); err != nil {
		return aoserrors.Wrap(err)
	}

	return aoserrors.Wrap(controller.saveEnv())
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *UbootController) mountEnv(device string, fstype string) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if controller.mountedPart == device {
		return nil
	}

	if controller.mountedPart != "" {
		if err = controller.umountEnv(); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	log.WithField("device", device).Debug("Mount env")

	if err = fs.Mount(device, envMountPoint, fstype, 0, ""); err != nil {
		return aoserrors.Wrap(err)
	}

	controller.mountedPart = device

	controller.mountTimer = time.AfterFunc(envMountTimeout, func() {
		if err := controller.umountEnv(); err != nil {
			log.Errorf("Can't umount env: %s", err)
		}
	})

	return nil
}

func (controller *UbootController) umountEnv() (umountErr error) {
	controller.Lock()
	defer controller.Unlock()

	if controller.mountedPart != "" {
		log.WithField("device", controller.mountedPart).Debug("Umount env")

		if controller.mountTimer != nil {
			controller.mountTimer.Stop()
			controller.mountTimer = nil
		}

		if err := fs.Umount(envMountPoint); err != nil {
			if umountErr == nil {
				umountErr = err
			}
		}

		controller.mountedPart = ""
	}

	return aoserrors.Wrap(umountErr)
}

func (controller *UbootController) getEnv() (err error) {
	if err = controller.mountEnv(controller.envDevice, controller.envFsType); err != nil {
		return aoserrors.Wrap(err)
	}

	imagePath := path.Join(envMountPoint, controller.envFileName)

	controller.cfg, err = ini.Load(imagePath)
	if err != nil {
		log.Errorf("Error loading env file %s err: %s", imagePath, aoserrors.Wrap(err))

		if err = controller.generateDefaultEnv(); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

func (controller *UbootController) saveEnv() (err error) {
	if err = controller.mountEnv(controller.envDevice, controller.envFsType); err != nil {
		return aoserrors.Wrap(err)
	}

	imagePath := path.Join(envMountPoint, controller.envFileName)

	if err = controller.cfg.SaveTo(imagePath); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (controller *UbootController) getVar(name string) (value int, err error) {
	if controller.cfg == nil {
		if err = controller.getEnv(); err != nil {
			return 0, aoserrors.Wrap(err)
		}
	}

	log.Debugf("UbootController: getting var %s, value %s", name, controller.cfg.Section("").Key(name).String())

	value, err = controller.cfg.Section("").Key(name).Int()

	return value, aoserrors.Wrap(err)
}

func (controller *UbootController) setVar(name string, value string) (err error) {
	if controller.cfg == nil {
		if err = controller.getEnv(); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	log.Debugf("UbootController: setting var %s, value %s", name, value)

	controller.cfg.Section("").Key(name).SetValue(value)

	return nil
}

func (controller *UbootController) generateDefaultEnv() (err error) {
	controller.cfg = ini.Empty()

	if err = controller.setVar(aosBoot1Ok, "1"); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = controller.setVar(aosBoot2Ok, "1"); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = controller.setVar(aosMainPart, strconv.Itoa(0)); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = controller.setVar(aosBootPart, strconv.Itoa(0)); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = controller.saveEnv(); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}
