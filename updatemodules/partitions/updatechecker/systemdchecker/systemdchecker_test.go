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

package systemdchecker_test

import (
	"aos_updatemanager/config"
	"aos_updatemanager/updatemodules/partitions/updatechecker/systemdchecker"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"testing"
	"time"

	"github.com/aoscloud/aos_common/aoserrors"
	log "github.com/sirupsen/logrus"
)

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

var tmpDir string

/***********************************************************************************************************************
 * Init
 **********************************************************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/***********************************************************************************************************************
 * Main
 **********************************************************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestServicesActivated(t *testing.T) {
	serviceList := []string{"um_test1.service", "um_test2.service", "um_test3.service"}

	for _, service := range serviceList {
		if err := createService(service, "sleep 10", "system"); err != nil {
			t.Fatalf("Can't create test service: %s", err)
		}
		defer removeService(service, "system")
	}

	if err := systemdchecker.New(systemdchecker.Config{
		SystemServices: serviceList, Timeout: config.Duration{Duration: 10 * time.Second},
	}).Check(); err != nil {
		t.Errorf("Watch services error: %s", err)
	}
}

func TestServicesFailed(t *testing.T) {
	serviceList := []string{"um_test1.service", "um_test2.service", "um_test3.service"}

	for _, service := range serviceList {
		if err := createService(service, `bash -c "exit 1"`, "system"); err != nil {
			t.Fatalf("Can't create test service: %s", err)
		}
		defer removeService(service, "system")
	}

	if err := systemdchecker.New(systemdchecker.Config{
		SystemServices: serviceList, Timeout: config.Duration{Duration: 10 * time.Second},
	}).Check(); err == nil {
		t.Error("Error expected")
	}
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func createService(name string, cmd, bus string) (err error) {
	defer func() {
		if err != nil {
			removeService(name, bus)
		}
	}()

	const serviceTemplate = `
[Service]
ExecStart=%s
`
	if err = ioutil.WriteFile(path.Join(tmpDir, name), []byte(fmt.Sprintf(serviceTemplate, cmd)), 0o644); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = systemctlCommand("link", path.Join(tmpDir, name), "--"+bus); err != nil {
		return aoserrors.Wrap(err)
	}

	if err = systemctlCommand("start", name, "--"+bus, "--no-block"); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func removeService(name string, bus string) {
	if err := systemctlCommand("stop", name, "--"+bus); err != nil {
		log.Errorf("Can't stop service: %s", err)
	}

	if err := systemctlCommand("disable", name, "--"+bus); err != nil {
		log.Errorf("Can't disable service: %s", err)
	}

	if err := os.RemoveAll(path.Join(tmpDir, name)); err != nil {
		log.Errorf("Can't remove service: %s", err)
	}
}

func systemctlCommand(params ...string) (err error) {
	output, err := exec.Command("systemctl", params...).CombinedOutput()
	if err != nil {
		return aoserrors.Errorf("error: %s, message: %s", err, string(output))
	}

	return nil
}
