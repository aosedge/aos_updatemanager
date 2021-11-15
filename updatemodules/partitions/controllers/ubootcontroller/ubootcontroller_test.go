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

package ubootcontroller_test

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/aoserrors"
	"gitpct.epam.com/epmd-aepr/aos_common/fs"
	"gitpct.epam.com/epmd-aepr/aos_common/utils/testtools"
	"gopkg.in/ini.v1"

	"aos_updatemanager/updatemodules/partitions/controllers/ubootcontroller"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const uEnvFile = "uEnv.txt"

/*******************************************************************************
 * Variables
 ******************************************************************************/

var tmpDir string

var disk *testtools.TestDisk

var envFileFormat = `
aos_boot_main=1
aos_boot1_ok=0
aos_boot2_ok=0
aos_boot_part=1
`

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	tmpDir, err := ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	if disk, err = testtools.NewTestDisk(
		path.Join(tmpDir, "testdisk.img"),
		[]testtools.PartDesc{
			{Type: "vfat", Label: "env", Size: 32},
		}); err != nil {
		log.Fatalf("Can't create test disk: %s", err)
	}

	if err = createEnvFile(disk.Partitions[0]); err != nil {
		log.Fatalf("Can't create test file %s", uEnvFile)
	}

	ret := m.Run()

	if err = disk.Close(); err != nil {
		log.Errorf("Can't close test disk: %s", err)
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Errorf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestEnv(t *testing.T) {
	controller, err := ubootcontroller.New(disk.Partitions[0].Device, uEnvFile)
	if err != nil {
		t.Fatalf("Unable to create ubootcontroller: %s", err)
	}
	defer controller.Close()

	cfg, err := readConfig(disk.Partitions[0])
	if err != nil {
		t.Errorf("readConfig error: %s", err)
	}

	index, err := controller.GetCurrentBoot()
	if err != nil {
		t.Errorf("GetCurrentBoot call failed: %s", err)
	}

	if index != getVar("aos_boot_part", cfg) {
		t.Error("Wrong value on GetCurrentBoot")
	}

	index, err = controller.GetMainBoot()
	if err != nil {
		t.Errorf("GetMainBoot call failed: %s", err)
	}

	if index != getVar("aos_boot_main", cfg) {
		t.Error("Wrong value on GetMainBoot")
	}

	err = controller.SetMainBoot(2)
	if err != nil {
		t.Errorf("SetMainBoot call failed")
	}

	if getVar("aos_boot1_ok", cfg) != 0 {
		t.Error("Error validating uEnv.txt params")
	}

	err = controller.SetBootOK()
	if err != nil {
		t.Errorf("SetBootOK call failed")
	}

	cfg, err = readConfig(disk.Partitions[0])
	if err != nil {
		t.Errorf("readConfig error %s", err)
	}

	if getVar("aos_boot1_ok", cfg) != 1 {
		t.Error("Error validating uEnv.txt params")
	}

	if getVar("aos_boot2_ok", cfg) != 1 {
		t.Error("Error validating uEnv.txt params")
	}

	index, err = controller.GetMainBoot()
	if err != nil {
		t.Errorf("GetMainBoot call failed: %s", err)
	}

	if index != getVar("aos_boot_main", cfg) {
		t.Error("Wrong value on GetMainBoot")
	}
}

func TestDefaultEnv(t *testing.T) {
	if err := createIncorrectEnvFile(disk.Partitions[0]); err != nil {
		t.Fatalf("Unable to create incorrect env file: %s", err)
	}

	controller, err := ubootcontroller.New(disk.Partitions[0].Device, uEnvFile)
	if err != nil {
		t.Fatalf("Unable to create ubootcontroller: %s", err)
	}
	defer controller.Close()

	index, err := controller.GetCurrentBoot()
	if err != nil {
		t.Errorf("Get current boot error: %s", err)
	}

	cfg, err := readConfig(disk.Partitions[0])
	if err != nil {
		t.Errorf("Read configuration error: %s", err)
	}

	if index != getVar("aos_boot_part", cfg) {
		t.Error("Wrong value of current boot")
	}

	index, err = controller.GetMainBoot()
	if err != nil {
		t.Errorf("Get main boot error: %s", err)
	}

	if index != getVar("aos_boot_main", cfg) {
		t.Error("Wrong value on main boot")
	}

	if getVar("aos_boot1_ok", cfg) != 1 {
		t.Error("Validation uEnv.txt params error")
	}

	if getVar("aos_boot2_ok", cfg) != 1 {
		t.Error("Validation uEnv.txt params error")
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func processEnvFile(part testtools.PartInfo, cb func(string) error) (err error) {
	mountPoint, err := ioutil.TempDir("", "uboot_*")
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer os.RemoveAll(mountPoint)

	if err = fs.Mount(part.Device, mountPoint, part.Type, 0, ""); err != nil {
		return aoserrors.Wrap(err)
	}
	defer func() {
		if err = fs.Umount(mountPoint); err != nil {
			log.Error("Unable to unmount partition")
		}
	}()

	fileName := path.Join(mountPoint, uEnvFile)

	if err = cb(fileName); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func createEnvFile(part testtools.PartInfo) (err error) {
	err = processEnvFile(part, func(fileName string) (err error) {
		err = ioutil.WriteFile(fileName, []byte(envFileFormat), 0644)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		return nil
	})

	return aoserrors.Wrap(err)
}

func createIncorrectEnvFile(part testtools.PartInfo) (err error) {
	err = processEnvFile(part, func(fileName string) (err error) {
		err = ioutil.WriteFile(fileName, []byte("@@@@@@"), 0644)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		return nil
	})

	return aoserrors.Wrap(err)
}

func readConfig(part testtools.PartInfo) (cfg *ini.File, err error) {
	var conf *ini.File
	err = processEnvFile(part, func(fileName string) (err error) {
		conf, err = ini.Load(fileName)
		return aoserrors.Wrap(err)
	})

	return conf, err
}

func getVar(name string, cfg *ini.File) (value int) {
	value, _ = cfg.Section("").Key(name).Int()
	return value
}
