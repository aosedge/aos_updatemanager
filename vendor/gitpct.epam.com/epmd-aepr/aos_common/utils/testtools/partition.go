// SPDX-License-Identifier: Apache-2.0
//
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

package testtools

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"gitpct.epam.com/epmd-aepr/aos_common/aoserrors"
)

// This package contains different tools which are used in unit tests by
// different modules

/*******************************************************************************
 * Consts
 ******************************************************************************/

const ioBufferSize = 1024 * 1024

/*******************************************************************************
 * Types
 ******************************************************************************/

// PartDesc partition description structure
type PartDesc struct {
	Type  string
	Label string
	Size  uint64
}

// PartInfo partition info structure
type PartInfo struct {
	PartDesc
	Device   string
	PartUUID string
}

// TestDisk test disk structure
type TestDisk struct {
	Device     string
	Partitions []PartInfo

	path string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// NewTestDisk creates new disk in file
func NewTestDisk(path string, desc []PartDesc) (disk *TestDisk, err error) {
	disk = &TestDisk{
		Partitions: make([]PartInfo, 0, len(desc)),
		path:       path}

	defer func(disk *TestDisk) {
		if err != nil {
			disk.Close()
		}
	}(disk)

	// skip 1M for GPT table etc. and add 1M after device
	var diskSize uint64 = 2

	for _, part := range desc {
		diskSize = diskSize + part.Size
	}

	var output []byte

	if output, err = exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M", "count="+strconv.FormatUint(diskSize, 10)).CombinedOutput(); err != nil {
		return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err = exec.Command("parted", "-s", path, "mktable", "gpt").CombinedOutput(); err != nil {
		return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	diskSize = 1

	for _, part := range desc {
		if output, err = exec.Command("parted", "-s", path, "mkpart", "primary",
			fmt.Sprintf("%dMiB", diskSize),
			fmt.Sprintf("%dMiB", diskSize+part.Size)).CombinedOutput(); err != nil {
			return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
		}

		diskSize = diskSize + part.Size
	}

	if output, err = exec.Command("losetup", "-f", "-P", path, "--show").CombinedOutput(); err != nil {
		return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
	}

	disk.Device = strings.TrimSpace(string(output))

	for i, part := range desc {
		info := PartInfo{
			PartDesc: part,
			Device:   disk.Device + "p" + strconv.Itoa(i+1),
		}

		if info.PartUUID, err = getPartUUID(info.Device); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		disk.Partitions = append(disk.Partitions, info)

		labelOption := "-L"

		if strings.Contains(part.Type, "fat") || strings.Contains(part.Type, "dos") {
			labelOption = "-n"
		}

		if output, err = exec.Command("mkfs."+part.Type, info.Device, labelOption, info.Label).CombinedOutput(); err != nil {
			return nil, aoserrors.Errorf("%s (%s)", err, (string(output)))
		}
	}

	return disk, nil
}

// Close closes test disk
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

// CreateFilePartition creates partition in file
func CreateFilePartition(path string, fsType string, size uint64,
	contentCreator func(mountPoint string) (err error), archivate bool) (err error) {
	var output []byte

	if output, err = exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M",
		"count="+strconv.FormatUint(size, 10)).CombinedOutput(); err != nil {
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

		if mountPoint, err = ioutil.TempDir("", "um_mount"); err != nil {
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

// ComparePartitions compares partitions
func ComparePartitions(dst, src string) (err error) {
	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_RDONLY, 0)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	srcMd5 := md5.New()
	dstMd5 := md5.New()

	size, err := srcFile.Seek(0, 2)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = srcFile.Seek(0, 0); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err := io.CopyN(srcMd5, srcFile, size); err != nil && err != io.EOF {
		return aoserrors.Wrap(err)
	}

	if _, err := io.CopyN(dstMd5, dstFile, size); err != nil && err != io.EOF {
		return aoserrors.Wrap(err)
	}

	if !reflect.DeepEqual(srcMd5.Sum(nil), dstMd5.Sum(nil)) {
		return aoserrors.New("data mismatch")
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

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
