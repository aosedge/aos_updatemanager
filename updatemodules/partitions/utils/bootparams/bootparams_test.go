// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2023 Renesas Electronics Corporation.
// Copyright (C) 2023 EPAM Systems, Inc.
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

package bootparams_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/aosedge/aos_updatemanager/updatemodules/partitions/utils/bootparams"
)

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

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	bootparams.BootParamsPath = filepath.Join(tmpDir, "cmdline")

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Errorf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/***********************************************************************************************************************
 * Tests
 **********************************************************************************************************************/

func TestBootParts(t *testing.T) {
	type testData struct {
		bootParams   string
		mode         string
		partSuffixes []string
		result       []string
		err          error
	}

	//nolint:goerr113
	data := []testData{
		{mode: "unknown", err: errors.New("unsupported mode: unknown")},
		{mode: "auto", err: errors.New("no root parameter found")},
		{mode: "auto", bootParams: "root=/dev/sda", err: errors.New("can't define root device")},
		{mode: "bootargs", err: errors.New("no aosupdate.boot.parta parameter found ")},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.parta",
			err: errors.New("no aosupdate.boot.partb parameter found"),
		},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.parta aosupdate.boot.partb",
			err: errors.New("aosupdate.boot.parta parameter empty"),
		},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.parta=/dev/sda1 aosupdate.boot.partb",
			err: errors.New("aosupdate.boot.partb parameter empty"),
		},
		{
			mode: "auto", bootParams: "root=/dev/sda3",
			partSuffixes: []string{"1", "2"},
			result:       []string{"/dev/sda1", "/dev/sda2"},
		},
		{
			mode: "auto", bootParams: "root=/dev/mmcblk1p3",
			partSuffixes: []string{"1", "2"},
			result:       []string{"/dev/mmcblk1p1", "/dev/mmcblk1p2"},
		},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.parta=/dev/mmcblk0p1 aosupdate.boot.partb=/dev/mmcblk0p2",
			result: []string{"/dev/mmcblk0p1", "/dev/mmcblk0p2"},
		},
		{
			mode: "manual", partSuffixes: []string{"/dev/hda1", "/dev/hda2"},
			result: []string{"/dev/hda1", "/dev/hda2"},
		},
	}

	for _, item := range data {
		if err := os.WriteFile(bootparams.BootParamsPath, []byte(item.bootParams), 0o600); err != nil {
			t.Fatalf("Can't write boot params: %v", err)
		}

		params, err := bootparams.New()
		if err != nil {
			t.Fatalf("Can't create boot params: %v", err)
		}

		result, err := params.GetBootParts(item.mode, item.partSuffixes)

		if item.err != nil {
			if err == nil || !strings.Contains(err.Error(), item.err.Error()) {
				t.Errorf("Unexpected error received: %v", err)
			}
		} else if err != nil {
			t.Errorf("Can't get boot parts: %v", err)
		}

		if !reflect.DeepEqual(item.result, result) {
			t.Errorf("Wrong result received: %v", result)
		}
	}
}

func TestEnvPart(t *testing.T) {
	type testData struct {
		bootParams string
		mode       string
		partSuffix string
		result     string
		err        error
	}

	//nolint:goerr113
	data := []testData{
		{mode: "unknown", err: errors.New("unsupported mode: unknown")},
		{mode: "auto", err: errors.New("no root parameter found")},
		{mode: "auto", bootParams: "root=/dev/sda", err: errors.New("can't define root device")},
		{mode: "bootargs", err: errors.New("no aosupdate.boot.envPart parameter found ")},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.envPart",
			err: errors.New("aosupdate.boot.envPart parameter empty"),
		},
		{
			mode: "auto", bootParams: "root=/dev/sda3",
			partSuffix: "3",
			result:     "/dev/sda3",
		},
		{
			mode: "auto", bootParams: "root=/dev/mmcblk1p3",
			partSuffix: "3",
			result:     "/dev/mmcblk1p3",
		},
		{
			mode: "bootargs", bootParams: "aosupdate.boot.envPart=/dev/mmcblk0p3",
			result: "/dev/mmcblk0p3",
		},
		{
			mode: "manual", partSuffix: "/dev/hda3", result: "/dev/hda3",
		},
	}

	for _, item := range data {
		if err := os.WriteFile(bootparams.BootParamsPath, []byte(item.bootParams), 0o600); err != nil {
			t.Fatalf("Can't write boot params: %v", err)
		}

		params, err := bootparams.New()
		if err != nil {
			t.Fatalf("Can't create boot params: %v", err)
		}

		result, err := params.GetEnvPart(item.mode, item.partSuffix)

		if item.err != nil {
			if err == nil || !strings.Contains(err.Error(), item.err.Error()) {
				t.Errorf("Unexpected error received: %v %v", err, item)
			}
		} else if err != nil {
			t.Errorf("Can't get env part: %v", err)
		}

		if !reflect.DeepEqual(item.result, result) {
			t.Errorf("Wrong result received: %v", result)
		}
	}
}
