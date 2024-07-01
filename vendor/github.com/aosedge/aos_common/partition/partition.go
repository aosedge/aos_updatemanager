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

package partition

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aosedge/aos_common/aoserrors"
)

// #cgo pkg-config: blkid
// #include <blkid.h>
import "C"

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	tagTypeLabel    = "LABEL"
	tagTypeFSType   = "TYPE"
	tagTypePartUUID = "PARTUUID"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// PartInfo partition info
type PartInfo struct {
	Device   string
	PartUUID string
	FSType   string
	Label    string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// GetPartInfo returns partition info
func GetPartInfo(partDevice string) (partInfo PartInfo, err error) {
	var (
		blkdev   C.blkid_dev
		blkcache C.blkid_cache
	)

	if ret := C.blkid_get_cache(&blkcache, C.CString("/dev/null")); ret != 0 { //nolint:gocritic // wrong warning?
		return PartInfo{}, aoserrors.New("can't get blkid cache")
	}

	if blkdev = C.blkid_get_dev(blkcache, C.CString(partDevice), C.BLKID_DEV_NORMAL); blkdev == nil {
		return PartInfo{}, aoserrors.New("can't get blkid device")
	}

	partInfo.Device = C.GoString(C.blkid_dev_devname(blkdev))

	iter := C.blkid_tag_iterate_begin(blkdev)

	var (
		tagType  *C.char
		tagValue *C.char
	)

	for C.blkid_tag_next(iter, &tagType, &tagValue) == 0 { //nolint:gocritic // wrong warning?
		switch C.GoString(tagType) {
		case tagTypeLabel:
			partInfo.Label = C.GoString(tagValue)

		case tagTypeFSType:
			partInfo.FSType = C.GoString(tagValue)

		case tagTypePartUUID:
			partInfo.PartUUID = C.GoString(tagValue)
		}
	}

	C.blkid_tag_iterate_end(iter)

	return partInfo, nil
}

// GetParentDevice returns partition parent device
// Example: input: /dev/nvme0n1p2 output: /dev/nvme0n1
func GetParentDevice(partitionPath string) (devPath string, err error) {
	partition, err := filepath.Rel("/dev", partitionPath)
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	items, err := os.ReadDir("/sys/block")
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	for _, item := range items {
		subItems, err := os.ReadDir(path.Join("/sys/block", item.Name()))
		if err != nil {
			return "", aoserrors.Wrap(err)
		}

		for _, subItem := range subItems {
			if subItem.Name() == partition {
				return path.Join("/dev", item.Name()), nil
			}
		}
	}

	return "", aoserrors.New("can't determine parent device")
}

// GetPartitionNum returns partition number
// Example: input: /dev/nvme0n1p2 output: 2
func GetPartitionNum(partitionPath string) (num int, err error) {
	partition, err := filepath.Rel("/dev", partitionPath)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}

	sysPath := fmt.Sprintf("/sys/class/block/%s/partition", partition)
	// Check if file exists
	if _, err := os.Stat(sysPath); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	b, err := os.ReadFile(sysPath)
	if err != nil || len(b) == 0 {
		return 0, aoserrors.Wrap(err)
	}

	num, err = strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return num, aoserrors.Wrap(err)
}
