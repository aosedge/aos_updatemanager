package partition

// #cgo pkg-config: blkid
// #include <blkid.h>
import "C"

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const retryCount = 3

const (
	tagTypeLabel    = "LABEL"
	tagTypeFSType   = "TYPE"
	tagTypePartUUID = "PARTUUID"
)

const ioBufferSize = 1024 * 1024

/*******************************************************************************
 * Types
 ******************************************************************************/

// Info partition info
type Info struct {
	Device   string
	Type     string
	Label    string
	PartUUID uuid.UUID
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// Mount creates mount point and mount device to it
func Mount(device string, mountPoint string, fsType string) (err error) {
	log.WithFields(log.Fields{"device": device, "type": fsType, "mountPoint": mountPoint}).Debug("Mount partition")

	if err = os.MkdirAll(mountPoint, 0755); err != nil {
		return err
	}

	if err = retry(
		func() error {
			return syscall.Mount(device, mountPoint, fsType, 0, "")
		},
		func(err error) {
			log.Warningf("Mount error: %s, try remount...", err)

			// Try to sync and force umount
			syscall.Unmount(mountPoint, syscall.MNT_FORCE)
		}); err != nil {
		return err
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
				err = removeErr
			}
		}
	}()

	if err = retry(
		func() error {
			syscall.Sync()

			return syscall.Unmount(mountPoint, 0)
		},
		func(err error) {
			log.Warningf("Umount error: %s, retry...", err)

			// Try to sync and force umount
			syscall.Sync()
		}); err != nil {
		return err
	}

	return nil
}

// Copy copies one partition to another
func Copy(dst, src string) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Copy partition")

	start := time.Now()

	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	if copied, err = io.Copy(dstFile, srcFile); err != nil {
		return 0, err
	}

	log.WithFields(log.Fields{"copied": copied, "duration": time.Since(start)}).Debug("Copy partition")

	return copied, nil
}

// CopyFromArchive copies partition from archived file
func CopyFromArchive(dst, src string) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Copy partition from archive")

	start := time.Now()

	srcFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return 0, err
	}
	defer gz.Close()

	buf := make([]byte, ioBufferSize)

	for err != io.EOF {
		var readCount int

		if readCount, err = gz.Read(buf); err != nil && err != io.EOF {
			return copied, err
		}

		if readCount > 0 {
			var writeCount int

			if writeCount, err = dstFile.Write(buf[:readCount]); err != nil {
				return copied, err
			}

			copied = copied + int64(writeCount)
		}
	}

	log.WithFields(log.Fields{"copied": copied, "duration": time.Since(start)}).Debug("Copy partition from archive")

	return copied, nil
}

// GetInfo returns partition info
func GetInfo(device string) (info Info, err error) {
	var (
		blkdev   C.blkid_dev
		blkcache C.blkid_cache
	)

	if ret := C.blkid_get_cache(&blkcache, C.CString("/dev/null")); ret != 0 {
		return Info{}, errors.New("can't get blkid cache")
	}

	if blkdev = C.blkid_get_dev(blkcache, C.CString(device), C.BLKID_DEV_NORMAL); blkdev == nil {
		return Info{}, errors.New("can't get blkid device")
	}

	info.Device = C.GoString(C.blkid_dev_devname(blkdev))

	iter := C.blkid_tag_iterate_begin(blkdev)

	var (
		tagType  *C.char
		tagValue *C.char
	)

	for C.blkid_tag_next(iter, &tagType, &tagValue) == 0 {
		switch C.GoString(tagType) {
		case tagTypeLabel:
			info.Label = C.GoString(tagValue)

		case tagTypeFSType:
			info.Type = C.GoString(tagValue)

		case tagTypePartUUID:
			var err error

			if info.PartUUID, err = uuid.Parse(C.GoString(tagValue)); err != nil {
				log.Errorf("Can't parse PARTUUID")
			}
		}
	}

	C.blkid_tag_iterate_end(iter)

	return info, nil
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
			return err
		}

		if restorer != nil {
			restorer(err)
		}

		i++
	}
}
