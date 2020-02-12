package statecontroller

import (
	"aos_updatemanager/utils/grub"
	"aos_updatemanager/utils/partition"
	"path"
	"strconv"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type grubEnv struct {
	grub       *grub.Instance
	mountPoint string
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *Controller) newGrubEnv(bootPart int) (env *grubEnv, err error) {
	env = &grubEnv{mountPoint: bootMountPoint + strconv.Itoa(bootPart)}

	partInfo := controller.config.BootPartitions[bootPart]

	if err = partition.Mount(partInfo.Device, env.mountPoint, partInfo.FSType); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if err := partition.Umount(env.mountPoint); err != nil {
				log.Errorf("Can't unmount boot partition: %s", err)
			}
		}
	}()

	if env.grub, err = grub.New(path.Join(env.mountPoint, controller.config.GRUBEnvFile)); err != nil {
		return nil, err
	}

	return env, nil
}

func (env *grubEnv) close() (err error) {
	err = env.grub.Close()

	if err := partition.Umount(env.mountPoint); err != nil {
		log.Errorf("Can't unmount boot partition: %s", err)
	}

	return err
}
