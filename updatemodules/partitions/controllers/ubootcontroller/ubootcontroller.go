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

package ubootcontroller

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/fs"
	"gitpct.epam.com/epmd-aepr/aos_common/partition"
	"gopkg.in/ini.v1"
)

/*******************************************************************************
 * Constants
 ******************************************************************************/

const envMountPoint = "/tmp/aos/env"
const envMountTimeout = 10 * time.Minute

const (
	aosBoot1Ok  = "aos_boot1_ok"
	aosBoot2Ok  = "aos_boot2_ok"
	aosBootPart = "aos_boot_part"
	aosMainPart = "aos_boot_main"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// UbootController instance
type UbootController struct {
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

var (
	errNotReady   = errors.New("controller not ready")
	errOutOfRange = errors.New("index out of range")
	errNotFound   = errors.New("index not found")
)

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new instance of UbootController
func New(envDevice string, envFileName string) (controller *UbootController, err error) {
	log.Debug("Create Uboot controller")

	if _, err := os.Stat(envDevice); os.IsNotExist(err) {
		log.Errorf("Can't create Uboot controller, wrong device %s", envDevice)
		return nil, err
	}

	info, err := partition.GetPartInfo(envDevice)
	if err != nil {
		return nil, err
	}

	controller = &UbootController{envDevice: envDevice, envFsType: info.FSType, envFileName: envFileName}

	// Unset PrettyFormat to avoid alignment
	ini.PrettyFormat = false

	// Reading environment file
	if err = controller.getEnv(); err != nil {
		return controller, err
	}

	return controller, nil
}

// Close closes UbootController
func (controller *UbootController) Close() {
	log.Debug("Close Uboot controller")
	if err := controller.umountEnv(); err != nil {
		log.Errorf("Error closing Uboot controller: %s", err)
	}
}

// GetCurrentBoot returns current boot part index
func (controller *UbootController) GetCurrentBoot() (index int, err error) {
	return controller.getVar(aosBootPart)
}

// GetMainBoot returns boot main part index
func (controller *UbootController) GetMainBoot() (index int, err error) {
	return controller.getVar(aosMainPart)
}

// SetMainBoot sets boot main part index
func (controller *UbootController) SetMainBoot(index int) (err error) {
	if err = controller.setVar(aosMainPart, strconv.Itoa(index)); err != nil {
		return err
	}

	return controller.saveEnv()
}

// SetBootOK sets boot successful flag
func (controller *UbootController) SetBootOK() (err error) {
	if err = controller.setVar(aosBoot1Ok, "1"); err != nil {
		return err
	}

	if err = controller.setVar(aosBoot2Ok, "1"); err != nil {
		return err
	}

	return controller.saveEnv()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *UbootController) mountEnv(device string, fstype string) (err error) {
	if controller.mountedPart == device {
		return nil
	}

	if controller.mountedPart != "" {
		if err = controller.umountEnv(); err != nil {
			return err
		}
	}

	log.WithField("device", device).Debug("Mount env")

	if err = fs.Mount(device, envMountPoint, fstype, 0, ""); err != nil {
		return err
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

	return umountErr
}

func (controller *UbootController) getEnv() (err error) {
	if err = controller.mountEnv(controller.envDevice, controller.envFsType); err != nil {
		return err
	}

	imagePath := path.Join(envMountPoint, controller.envFileName)
	//Create file if not exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		fd, err := os.Create(imagePath)
		if err != nil {
			return fmt.Errorf("can't create env file: %s", err)
		}

		fd.Close()

		return nil
	}

	controller.cfg, err = ini.Load(imagePath)
	if err != nil {
		return fmt.Errorf("error loading env file %s", imagePath)
	}

	return nil
}

func (controller *UbootController) saveEnv() (err error) {
	if err = controller.mountEnv(controller.envDevice, controller.envFsType); err != nil {
		return err
	}

	imagePath := path.Join(envMountPoint, controller.envFileName)

	controller.cfg.SaveTo(imagePath)

	return nil
}

func (controller *UbootController) getVar(name string) (value int, err error) {
	log.Debugf("UbootController: getting var %s, value %s", name,
		controller.cfg.Section("").Key(name).String())

	return controller.cfg.Section("").Key(name).Int()
}

func (controller *UbootController) setVar(name string, value string) (err error) {
	log.Debugf("UbootController: setting var %s, value %s", name,
		value)

	controller.cfg.Section("").Key(name).SetValue(value)

	return nil
}
