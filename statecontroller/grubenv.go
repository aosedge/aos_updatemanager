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

func (env *grubEnv) getGrubCfgVersion() (version uint64, err error) {
	versionStr, err := env.grub.GetVariable(grubCfgVersionVar)
	if err != nil {
		return 0, err
	}

	if version, err = strconv.ParseUint(versionStr, 10, 64); err != nil {
		return 0, err
	}

	return version, nil
}

func (env *grubEnv) getImageVersion() (version uint64, err error) {
	versionStr, err := env.grub.GetVariable(grubImageVersionVar)
	if err != nil {
		return 0, err
	}

	if version, err = strconv.ParseUint(versionStr, 10, 64); err != nil {
		return 0, err
	}

	return version, nil
}

func (env *grubEnv) setImageVersion(version uint64) (err error) {
	if err = env.grub.SetVariable(grubImageVersionVar, strconv.FormatUint(version, 10)); err != nil {
		return err
	}

	return nil
}

func (env *grubEnv) getDefaultBootIndex() (index int, err error) {
	indexStr, err := env.grub.GetVariable(grubDefaultBootIndexVar)
	if err != nil {
		return 0, err
	}

	index64, err := strconv.ParseInt(indexStr, 10, 0)
	if err != nil {
		return 0, err
	}

	return int(index64), nil
}

func (env *grubEnv) setDefaultBootIndex(index int) (err error) {
	if err = env.grub.SetVariable(grubDefaultBootIndexVar, strconv.FormatInt(int64(index), 10)); err != nil {
		return err
	}

	return nil
}

func (env *grubEnv) getFallbackBootIndex() (index int, err error) {
	indexStr, err := env.grub.GetVariable(grubFallbackBootIndexVar)
	if err != nil {
		return 0, err
	}

	index64, err := strconv.ParseInt(indexStr, 10, 0)
	if err != nil {
		return 0, err
	}

	return int(index64), nil
}

func (env *grubEnv) setFallbackBootIndex(index int) (err error) {
	if err = env.grub.SetVariable(grubFallbackBootIndexVar, strconv.FormatInt(int64(index), 10)); err != nil {
		return err
	}

	return nil
}

func (env *grubEnv) close() (err error) {
	err = env.grub.Close()

	if err := partition.Umount(env.mountPoint); err != nil {
		log.Errorf("Can't unmount boot partition: %s", err)
	}

	return err
}
