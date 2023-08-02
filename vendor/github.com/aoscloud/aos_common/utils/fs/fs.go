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

package fs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anexia-it/fsquota"
	log "github.com/sirupsen/logrus"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/aoscloud/aos_common/utils/retryhelper"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	retryCount = 3
	retryDelay = 1 * time.Second
)

const folderPerm = 0o755

const statBlockSize = 512

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// Mount creates mount point and mount source to it.
func Mount(source string, mountPoint string, fsType string, flags uintptr, opts string) error {
	log.WithFields(log.Fields{"source": source, "type": fsType, "mountPoint": mountPoint}).Debug("Mount dir")

	if err := os.MkdirAll(mountPoint, folderPerm); err != nil {
		return aoserrors.Wrap(err)
	}

	if err := retryhelper.Retry(context.Background(), func() error {
		return aoserrors.Wrap(syscall.Mount(source, mountPoint, fsType, flags, opts))
	}, func(retryCount int, delay time.Duration, err error) {
		log.Warningf("Mount error: %s, try remount...", err)

		forceUmount(mountPoint)
	}, retryCount, retryDelay, 0); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// OverlayMount creates mount point and mount overlay FS to it.
func OverlayMount(mountPoint string, lowerDirs []string, workDir, upperDir string) error {
	opts := "lowerdir=" + strings.Join(lowerDirs, ":")

	if upperDir != "" {
		if workDir == "" {
			return aoserrors.New("working dir path should be set")
		}

		if err := os.RemoveAll(workDir); err != nil {
			return aoserrors.Wrap(err)
		}

		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return aoserrors.Wrap(err)
		}

		opts = opts + ",workdir=" + workDir + ",upperdir=" + upperDir
	}

	if err := Mount("overlay", mountPoint, "overlay", 0, opts); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// Umount umount mount point and remove it.
func Umount(mountPoint string) (err error) {
	log.WithFields(log.Fields{"mountPoint": mountPoint}).Debug("Umount dir")

	defer func() {
		if removeErr := os.RemoveAll(mountPoint); removeErr != nil {
			log.Errorf("Can't remove mount point: %s", removeErr)

			if err == nil {
				err = aoserrors.Wrap(removeErr)
			}
		}
	}()

	if err = retryhelper.Retry(context.Background(), func() error {
		syscall.Sync()

		return aoserrors.Wrap(syscall.Unmount(mountPoint, 0))
	}, func(retryCount int, delay time.Duration, err error) {
		log.Warningf("Unmount error: %s, retry...", err)

		forceUmount(mountPoint)
	}, retryCount, retryDelay, 0); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// GetMountPoint returns mount point for directory.
func GetMountPoint(dir string) (mountPoint string, err error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return "", aoserrors.Wrap(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		relPath, err := filepath.Rel(fields[1], dir)
		if err != nil || strings.Contains(relPath, "..") {
			continue
		}

		if len(fields[1]) > len(mountPoint) {
			mountPoint = fields[1]
		}
	}

	if mountPoint == "" {
		return "", aoserrors.Errorf("failed to find mount point for %s", dir)
	}

	return mountPoint, nil
}

// GetAvailableSize returns the amount of memory used by the partition.
func GetAvailableSize(dir string) (availableSize int64, err error) {
	var stat syscall.Statfs_t

	if err = syscall.Statfs(dir, &stat); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return int64(stat.Bavail) * stat.Bsize, nil
}

// GetTotalSize returns total partition size.
func GetTotalSize(dir string) (totalSize int64, err error) {
	var stat syscall.Statfs_t

	if err = syscall.Statfs(dir, &stat); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return int64(stat.Blocks) * stat.Bsize, nil
}

// GetDirSize returns the size of directory.
func GetDirSize(path string) (size int64, err error) {
	if err = filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return aoserrors.Wrap(err)
		}

		var stat syscall.Stat_t

		if err = syscall.Lstat(path, &stat); err != nil {
			return aoserrors.Wrap(err)
		}

		size += stat.Blocks * statBlockSize

		return nil
	}); err != nil {
		return 0, aoserrors.Wrap(err)
	}

	return size, nil
}

// GetUserFSQuotaUsage gets file system user usage.
func GetUserFSQuotaUsage(path string, uid, gid uint32) (byteUsed uint64, err error) {
	if supported, _ := fsquota.UserQuotasSupported(path); !supported {
		return byteUsed, nil
	}

	user := user.User{Uid: fmt.Sprint(uid), Gid: fmt.Sprint(gid)}

	info, err := fsquota.GetUserInfo(path, &user)
	if err != nil {
		return byteUsed, aoserrors.Wrap(err)
	}

	return info.BytesUsed, nil
}

// SetUserFSQuota sets file system quota for user.
func SetUserFSQuota(path string, limit uint64, uid, gid uint32) (err error) {
	supported, _ := fsquota.UserQuotasSupported(path)

	if limit == 0 && !supported {
		return nil
	}

	user := user.User{Uid: fmt.Sprint(uid), Gid: fmt.Sprint(gid)}

	log.WithFields(log.Fields{"uid": uid, "gid": uid, "limit": limit}).Debug("Set user FS quota")

	limits := fsquota.Limits{}

	limits.Bytes.SetHard(limit)

	if _, err := fsquota.SetUserQuota(path, &user, limits); err != nil { //nolint:govet
		return aoserrors.Wrap(err)
	}

	return nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func forceUmount(mountPoint string) {
	syscall.Sync()
	_ = syscall.Unmount(mountPoint, syscall.MNT_FORCE)
}
