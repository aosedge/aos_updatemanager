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

func (env *grubEnv) init(controller *Controller) (err error) {
	log.Debug("Initialize GRUB variables")

	if err = env.grub.SetVariable(grubVersionVar, strconv.FormatUint(controller.version, 10)); err != nil {
		return err
	}

	if err = env.grub.SetVariable(grubBootIndexVar, strconv.FormatInt(int64(controller.grubBootIndex), 10)); err != nil {
		return err
	}

	return nil
}

func (env *grubEnv) getVersion() (version uint64, err error) {
	versionStr, err := env.grub.GetVariable(grubVersionVar)
	if err != nil {
		return 0, err
	}

	if version, err = strconv.ParseUint(versionStr, 10, 64); err != nil {
		return 0, err
	}

	return version, nil
}

func (env *grubEnv) getBootIndex() (index int, err error) {
	rootIndexStr, err := env.grub.GetVariable(grubBootIndexVar)
	if err != nil {
		return 0, err
	}

	index64, err := strconv.ParseInt(rootIndexStr, 10, 0)
	if err != nil {
		return 0, err
	}

	return int(index64), nil
}

func (env *grubEnv) close() (err error) {
	err = env.grub.Close()

	if err := partition.Umount(env.mountPoint); err != nil {
		log.Errorf("Can't unmount boot partition: %s", err)
	}

	return err
}
