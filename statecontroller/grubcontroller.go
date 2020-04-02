package statecontroller

import (
	"errors"
	"io/ioutil"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/grub"
	"aos_updatemanager/utils/partition"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	kernelRootPrefix      = "root="
	kernelBootIndexPrefix = "NUANCE.bootIndex="
)

const bootMountPoint = "/tmp/aos/boot"
const grubEnvFile = "EFI/BOOT/NUANCE/grubenv"

const bootMountTimeout = 10 * time.Minute

const grubConfigVersion = 2

const (
	envGrubConfigVersion = "NUANCE_GRUB_CFG_VERSION"
	envGrubBootOrder     = "NUANCE_BOOT_ORDER"
	envGrubActivePrefix  = "NUANCE_ACTIVE_"
	envGrubBootOKPrefix  = "NUANCE_BOOT_OK_"
	envGrubBootNext      = "NUANCE_BOOT_NEXT"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// GrubController GRUB controller instance
type GrubController struct {
	sync.Mutex

	mountTimer  *time.Timer
	boot        bootInfoProvider
	mountedPart string
	envVar      *grub.Instance

	partCount        int
	currentBootInfo  partition.Info
	currentRootIndex int

	bootNext int

	ready bool
	wg    *sync.WaitGroup
}

type bootInfoProvider interface {
	getNextBootPartition() (partInfo partition.Info, err error)
	getCurrentBootPartition() (partInfo partition.Info, err error)
}

/*******************************************************************************
 * Interface
 ******************************************************************************/

// WaitForReady waits for controller to be ready
func (controller *GrubController) WaitForReady() (err error) {
	controller.Lock()
	defer controller.Unlock()

	if controller.currentBootInfo, err = controller.boot.getCurrentBootPartition(); err != nil {
		return err
	}

	// Check GRUB config version

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return err
	}

	valueStr, err := envVar.GetVariable(envGrubConfigVersion)
	if err != nil {
		return err
	}

	configVersion, err := strconv.Atoi(valueStr)
	if err != nil {
		return err
	}

	log.WithField("version", configVersion).Debug("GRUB config version")

	if configVersion != grubConfigVersion {
		return errors.New("GRUB config version mismatch")
	}

	controller.ready = true

	return nil
}

// GetCurrentBoot returns current boot index
func (controller *GrubController) GetCurrentBoot() (index int, err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return 0, errNotReady
	}

	return controller.currentRootIndex, nil
}

// SetBootActive sets boot item as active
func (controller *GrubController) SetBootActive(index int, active bool) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return errNotReady
	}

	if index < 0 || index >= controller.partCount {
		return errOutOfRange
	}

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return err
	}

	value := 0

	if active {
		value = 1
	}

	if err = envVar.SetVariable(envGrubActivePrefix+strconv.Itoa(index), strconv.Itoa(value)); err != nil {
		return err
	}

	if err = envVar.Store(); err != nil {
		return err
	}

	return nil
}

// GetBootActive returns boot item active state
func (controller *GrubController) GetBootActive(index int) (active bool, err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return false, errNotReady
	}

	if index < 0 || index >= controller.partCount {
		return false, errOutOfRange
	}

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return false, err
	}

	valueStr, err := envVar.GetVariable(envGrubActivePrefix + strconv.Itoa(index))
	if err != nil {
		return false, err
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return false, err
	}

	if value != 0 {
		active = true
	}

	return active, nil
}

// GetBootOrder returns boot order
func (controller *GrubController) GetBootOrder() (indexes []int, err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return nil, errNotReady
	}

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return nil, err
	}

	valueStr, err := envVar.GetVariable(envGrubBootOrder)
	if err != nil {
		return nil, err
	}

	list := strings.Fields(valueStr)

	indexes = make([]int, 0, len(list))

	for _, item := range list {
		index, err := strconv.Atoi(item)
		if err != nil {
			return nil, err
		}

		if index < 0 || index >= controller.partCount {
			return nil, errOutOfRange
		}

		indexes = append(indexes, index)
	}

	return indexes, nil
}

// SetBootOrder sets boot order
func (controller *GrubController) SetBootOrder(indexes []int) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return errNotReady
	}

	valueStr := ""

	for _, index := range indexes {
		if index < 0 || index >= controller.partCount {
			return errOutOfRange
		}

		if valueStr != "" {
			valueStr = valueStr + " "
		}

		valueStr = valueStr + strconv.Itoa(index)
	}

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return err
	}

	if err = envVar.SetVariable(envGrubBootOrder, valueStr); err != nil {
		return err
	}

	if err = envVar.Store(); err != nil {
		return err
	}

	return nil
}

// SetBootNext sets next boot item
func (controller *GrubController) SetBootNext(index int) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return errNotReady
	}

	if index < 0 || index >= controller.partCount {
		return errOutOfRange
	}

	controller.bootNext = index

	return nil
}

// ClearBootNext clears next boot item
func (controller *GrubController) ClearBootNext() (err error) {
	controller.Lock()
	defer controller.Unlock()

	if !controller.ready {
		return errNotReady
	}

	envVar, err := controller.mountGrubEnv(controller.currentBootInfo)
	if err != nil {
		return err
	}

	if err = envVar.UnsetVariable(envGrubBootNext); err != nil {
		return err
	}

	if err = envVar.Store(); err != nil {
		return err
	}

	controller.bootNext = -1

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newGrubController(rootParts []string, cmdLineFile string,
	boot bootInfoProvider, wg *sync.WaitGroup) (controller *GrubController, err error) {
	log.Debug("Create GRUB controller")

	controller = &GrubController{boot: boot, partCount: len(rootParts), bootNext: -1, wg: wg}

	if controller.currentRootIndex, err = getCurrentPartIndex(cmdLineFile, rootParts); err != nil {
		return nil, err
	}

	return controller, nil
}

func (controller *GrubController) close() (err error) {
	controller.Lock()
	defer controller.Unlock()

	log.Debug("Close GRUB controller")

	if applyNextErr := controller.applyBootNext(); applyNextErr != nil {
		log.Errorf("Can't apply boot next: %s", applyNextErr)

		if err == nil {
			err = applyNextErr
		}
	}

	if umountErr := controller.umountGrubEnv(); umountErr != nil {
		log.Errorf("Can't umount GRUB env: %s", umountErr)

		if err == nil {
			err = umountErr
		}
	}

	return err
}

func getCurrentPartIndex(cmdLineFile string, rootParts []string) (index int, err error) {
	cmdLineData, err := ioutil.ReadFile(cmdLineFile)
	if err != nil {
		return 0, err
	}

	options := strings.Fields(string(cmdLineData))

	curPartString := ""
	curBootIndex := -1

	for _, option := range options {
		option = strings.TrimSpace(option)

		switch {
		case strings.HasPrefix(option, kernelRootPrefix):
			curPartString = strings.TrimPrefix(option, kernelRootPrefix)

		case strings.HasPrefix(option, kernelBootIndexPrefix):
			if curBootIndex, err = strconv.Atoi(strings.TrimPrefix(option, kernelBootIndexPrefix)); err != nil {
				return 0, err
			}
		}
	}

	if curBootIndex < 0 {
		return 0, errors.New("kernel boot index not found")
	}

	if curPartString == "" {
		return 0, errors.New("kernel root option not found")
	}

	log.WithFields(log.Fields{"index": curBootIndex, "part": curPartString}).Debug("GRUB current boot")

	for i, part := range rootParts {
		if curPartString == part {
			if i != curBootIndex {
				return 0, errors.New("wrong current boot index")
			}

			return i, nil
		}
	}

	return 0, errors.New("current root partition not configured")
}

func (controller *GrubController) mountGrubEnv(part partition.Info) (env *grub.Instance, err error) {
	if controller.mountedPart == part.Device {
		return controller.envVar, nil
	}

	if controller.mountedPart != "" {
		if err = controller.umountGrubEnv(); err != nil {
			return nil, err
		}
	}

	log.WithField("device", part.Device).Debug("Mount GRUB env")

	if err = partition.Mount(part.Device, bootMountPoint, part.Type); err != nil {
		return nil, err
	}

	controller.mountedPart = part.Device

	controller.mountTimer = time.AfterFunc(bootMountTimeout, func() {
		controller.Lock()
		defer controller.Unlock()

		if err := controller.umountGrubEnv(); err != nil {
			log.Errorf("Can't umount GRUB env: %s", err)
		}
	})

	if controller.envVar, err = grub.New(path.Join(bootMountPoint, grubEnvFile)); err != nil {
		if err := controller.umountGrubEnv(); err != nil {
			log.Errorf("Can't umount GRUB env: %s", err)
		}

		return nil, err
	}

	return controller.envVar, nil
}

func (controller *GrubController) umountGrubEnv() (umountErr error) {
	if controller.mountedPart != "" {
		log.WithField("device", controller.mountedPart).Debug("Umount GRUB env")

		controller.mountTimer.Stop()
		controller.mountTimer = nil

		if controller.envVar != nil {
			if err := controller.envVar.Close(); err != nil {
				if umountErr == nil {
					umountErr = err
				}
			}
		}

		controller.envVar = nil

		if err := partition.Umount(bootMountPoint); err != nil {
			if umountErr == nil {
				umountErr = err
			}
		}

		controller.mountedPart = ""
	}

	return umountErr
}

func (controller *GrubController) applyBootNext() (err error) {
	if controller.bootNext == -1 {
		return nil
	}

	log.Debug("GRUB controller: apply boot next")

	nextBootPart, err := controller.boot.getNextBootPartition()
	if err != nil {
		return err
	}

	envVar, err := controller.mountGrubEnv(nextBootPart)
	if err != nil {
		return err
	}

	if err = envVar.SetVariable(envGrubBootNext, strconv.Itoa(controller.bootNext)); err != nil {
		return err
	}

	if err = envVar.Store(); err != nil {
		return err
	}

	controller.bootNext = -1

	return nil
}
