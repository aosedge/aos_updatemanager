// SPDX-License-Identifier: Apache-2.0
//
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

package testtools

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/aoscloud/aos_common/aoserrors"
)

// This package contains different tools which are used in unit tests by
// different modules

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const strconvBase10 = 10

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// PartDesc partition description structure.
type PartDesc struct {
	Type  string
	Label string
	Size  uint64
}

// PartInfo partition info structure.
type PartInfo struct {
	PartDesc
	Device   string
	PartUUID string
}

// TestDisk test disk structure.
type TestDisk struct {
	Device     string
	Partitions []PartInfo

	path string
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// NewTestDisk creates new disk in file.
func NewTestDisk(path string, desc []PartDesc) (disk *TestDisk, err error) {
	disk = &TestDisk{
		Partitions: make([]PartInfo, 0, len(desc)),
		path:       path,
	}

	defer func(disk *TestDisk) {
		if err != nil {
			disk.Close()
		}
	}(disk)

	// skip 1M for GPT table etc. and add 1M after device
	var diskSize uint64 = 2

	for _, part := range desc {
		diskSize += part.Size
	}

	if err = createDisk(path, diskSize); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = createParts(path, desc); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if disk.Device, err = setupDevice(path); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if disk.Partitions, err = formatDisk(disk.Device, desc); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return disk, nil
}

// Close closes test disk.
func (disk *TestDisk) Close() (err error) {
	var output []byte

	if disk.Device != "" {
		if output, err = exec.Command("losetup", "-d", disk.Device).CombinedOutput(); err != nil {
			return aoserrors.Errorf("%s (%s)", err, (string(output)))
		}
	}

	if err = os.RemoveAll(disk.path); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// CreateFilePartition creates partition in file.
func CreateFilePartition(
	path string, fsType string, size uint64, contentCreator func(mountPoint string) (err error), archivate bool,
) (err error) {
	var output []byte

	if output, err = exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M",
		"count="+strconv.FormatUint(size, strconvBase10)).CombinedOutput(); err != nil {
		return aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err = exec.Command("mkfs."+fsType, path).CombinedOutput(); err != nil {
		return aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	if archivate {
		defer func() {
			if output, err = exec.Command("gzip", "-k", "-f", path).CombinedOutput(); err != nil {
				err = aoserrors.Errorf("%s (%s)", err, (string(output)))
			}
		}()
	}

	if contentCreator != nil {
		var mountPoint string

		if mountPoint, err = os.MkdirTemp("", "um_mount"); err != nil {
			return aoserrors.Wrap(err)
		}

		defer func() {
			if output, err := exec.Command("sync").CombinedOutput(); err != nil {
				log.Errorf("Sync error: %s", aoserrors.Errorf("%s (%s)", err, (string(output))))
			}

			if output, err := exec.Command("umount", mountPoint).CombinedOutput(); err != nil {
				log.Errorf("Umount error: %s", aoserrors.Errorf("%s (%s)", err, (string(output))))
			}

			if err := os.RemoveAll(mountPoint); err != nil {
				log.Errorf("Remove error: %s", err)
			}
		}()

		if output, err = exec.Command("mount", path, mountPoint).CombinedOutput(); err != nil {
			return aoserrors.Errorf("%s (%s)", err, (string(output)))
		}

		if err = contentCreator(mountPoint); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func getPartUUID(device string) (partUUID string, err error) {
	var output []byte

	if output, err = exec.Command("blkid", device).CombinedOutput(); err != nil {
		return "", aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	for _, field := range strings.Fields(string(output)) {
		if strings.HasPrefix(field, "PARTUUID=") {
			return strings.Trim(strings.TrimPrefix(field, "PARTUUID="), `"`), nil
		}
	}

	return "", aoserrors.New("partition UUID not found")
}

func createDisk(path string, size uint64) (err error) {
	var output []byte

	if output, err = exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M",
		"count="+strconv.FormatUint(size, strconvBase10)).CombinedOutput(); err != nil {
		return aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err = exec.Command("parted", "-s", path, "mktable", "gpt").CombinedOutput(); err != nil {
		return aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	return nil
}

func createParts(path string, desc []PartDesc) (err error) {
	var (
		diskSize uint64 = 1
		output   []byte
	)

	for _, part := range desc {
		if output, err = exec.Command("parted", "-s", path, "mkpart", "primary",
			fmt.Sprintf("%dMiB", diskSize),
			fmt.Sprintf("%dMiB", diskSize+part.Size)).CombinedOutput(); err != nil {
			return aoserrors.Errorf("%s (%s)", err, (string(output)))
		}

		diskSize += part.Size
	}

	return nil
}

func setupDevice(path string) (device string, err error) {
	var output []byte

	if output, err = exec.Command("losetup", "-f", "-P", path, "--show").CombinedOutput(); err != nil {
		return "", aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}

func formatDisk(device string, desc []PartDesc) (parts []PartInfo, err error) {
	var output []byte

	for i, part := range desc {
		info := PartInfo{
			PartDesc: part,
			Device:   device + "p" + strconv.Itoa(i+1),
		}

		if info.PartUUID, err = getPartUUID(info.Device); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		parts = append(parts, info)

		labelOption := "-L"

		if strings.Contains(part.Type, "fat") || strings.Contains(part.Type, "dos") {
			labelOption = "-n"
		}

		if output, err = exec.Command("mkfs."+part.Type, info.Device,
			labelOption, info.Label).CombinedOutput(); err != nil {
			return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
		}
	}

	return parts, nil
}
