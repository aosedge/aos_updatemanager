package partition

import (
	"os"
	"syscall"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const retryCount = 3

/*******************************************************************************
 * Public
 ******************************************************************************/

// Mount creates mount point and mount device to it
func Mount(device string, mountPoint string, fsType string) (err error) {
	log.WithFields(log.Fields{"device": device, "mountPoint": mountPoint}).Debug("Mount partition")

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
			log.Errorf("Can't remove")
			if err == nil {
				err = removeErr
			}
		}
	}()

	if err = retry(
		func() error {
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

/*******************************************************************************
 * Public
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
