package statecontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/partition"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	rootFSModuleID     = "rootfs"
	bootloaderModuleID = "bootloader"
)

const (
	kernelRootPrefix      = "root="
	kernelVersionPrefix   = "NUANCE.version="
	kernelBootIndexPrefix = "NUANCE.bootIndex="
	kernelBootFormat      = "(hd%d,gpt%d)"
)

const bootMountPoint = "/tmp/aos/boot"

const (
	grubCfgVersionVar        = "NUANCE_GRUB_CFG_VERSION"
	grubImageVersionVar      = "NUANCE_IMAGE_VERSION"
	grubSwitchVar            = "NUANCE_TRY_SWITCH"
	grubDefaultBootIndexVar  = "NUANCE_DEFAULT_BOOT_INDEX"
	grubFallbackBootIndexVar = "NUANCE_FALLBACK_BOOT_INDEX"
	grubBootOK               = "NUANCE_BOOT_OK"
)

const grubCfgVersion = 1

const (
	upgradeFinished = iota
	upgradeStarted
	upgradeTrySwitch
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller state controller instance
type Controller struct {
	moduleProvider        ModuleProvider
	efiProvider           EFIProvider
	config                controllerConfig
	state                 controllerState
	version               uint64
	grubCurrentBootIndex  int
	grubFallbackBootIndex int
	activeRootPart        int
	activeBootPart        int
	bootEFIIds            []uint16
	bootPartInfo          []partition.Info
	rootPartInfo          []partition.Info
	systemStatus          systemStatus
	sync.Mutex
}

// ModuleProvider module provider interface
type ModuleProvider interface {
	// GetModuleByID returns module by id
	GetModuleByID(id string) (module interface{}, err error)
}

// EFIProvider provider for accessing EFI vars
type EFIProvider interface {
	GetBootByPartUUID(partUUID uuid.UUID) (id uint16, err error)
	GetBootCurrent() (bootCurrent uint16, err error)
	GetBootNext() (bootNext uint16, err error)
	SetBootNext(bootNext uint16) (err error)
	DeleteBootNext() (err error)
	GetBootOrder() (bootOrder []uint16, err error)
	SetBootOrder(bootOrder []uint16) (err error)
}

type controllerConfig struct {
	KernelCmdline  string
	StateFile      string
	GRUBEnvFile    string
	RootPartitions []string
	BootPartitions []string
	PerformReboot  bool
}

type controllerState struct {
	UpgradeState   int
	UpgradeStatus  string
	UpgradeVersion uint64
	UpgradeModules map[string]string
	GrubBootIndex  int
	EFIBootIndex   int
}

type fsModule interface {
	SetPartitionForUpdate(path, fsType string) (err error)
	Upgrade(path string) (err error)
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new state controller instance
func New(configJSON []byte, moduleProvider ModuleProvider, efiProvider EFIProvider) (controller *Controller, err error) {
	log.Info("Create state constoller")

	if moduleProvider == nil {
		return nil, errors.New("module provider should not be nil")
	}

	controller = &Controller{
		moduleProvider: moduleProvider,
		efiProvider:    efiProvider,
		config: controllerConfig{
			GRUBEnvFile:   "EFI/BOOT/NUANCE/grubenv",
			KernelCmdline: "/proc/cmdline",
		},
	}

	controller.systemStatus.cond = sync.NewCond(controller)

	if err = json.Unmarshal(configJSON, &controller.config); err != nil {
		return nil, err
	}

	if err = controller.updatePartInfo(); err != nil {
		return nil, err
	}

	if err = controller.parseBootCmd(); err != nil {
		return nil, err
	}

	if err = controller.initState(); err != nil {
		return nil, err
	}

	if controller.state.UpgradeState != upgradeFinished {
		if err = controller.initModules(controller.state.UpgradeModules); err != nil {
			return nil, err
		}
	}

	go controller.systemCheck()

	return controller, nil
}

// Close closes state controller instance
func (controller *Controller) Close() (err error) {
	log.Info("Close state constoller")

	return nil
}

// GetVersion returns current installed image version
func (controller *Controller) GetVersion() (version uint64, err error) {
	return controller.version, nil
}

// GetPlatformID returns platform ID
func (controller *Controller) GetPlatformID() (id string, err error) {
	return "Nuance-OTA", nil
}

// Upgrade notifies state controller about start of system upgrade
func (controller *Controller) Upgrade(version uint64, modules map[string]string) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if err = controller.waitSystemCheckFinished(); err != nil {
		return err
	}

	log.WithField("version", version).Debug("State controller upgrade")

	if controller.state.UpgradeState != upgradeFinished {
		if controller.state.UpgradeVersion != version {
			return errors.New("another upgrade is in progress")
		}

		return nil
	}

	if err = controller.initModules(modules); err != nil {
		return err
	}

	if err = controller.startUpgrade(version, modules); err != nil {
		return err
	}

	return nil
}

// Revert notifies state controller about start of system revert
func (controller *Controller) Revert(version uint64, moduleIds []string) (err error) {
	controller.Lock()
	defer controller.Unlock()

	log.WithField("version", version).Debug("State controller revert")

	return errors.New("revert operation is not supported")
}

// UpgradeFinished notifies state controller about finish of upgrade
func (controller *Controller) UpgradeFinished(version uint64, status error,
	moduleStatus map[string]error) (postpone bool, err error) {
	controller.Lock()
	defer controller.Unlock()

	if err = controller.waitSystemCheckFinished(); err != nil {
		return false, err
	}

	log.WithField("version", version).Debug("State controller upgrade finished")

	if controller.state.UpgradeVersion != version {
		return false, errors.New("upgrade version mistmatch")
	}

	if controller.state.UpgradeState == upgradeFinished {
		if controller.state.UpgradeStatus != "" {
			return false, errors.New(controller.state.UpgradeStatus)
		}

		return false, nil
	}

	if status != nil {
		return false, controller.finishUpgrade(status)
	}

	if isModuleUpgraded(rootFSModuleID, controller.state.UpgradeModules) {
		if postpone, err = controller.finishRootFSUpgrade(); err != nil {
			return false, controller.finishUpgrade(err)
		}
	}

	if isModuleUpgraded(bootloaderModuleID, controller.state.UpgradeModules) {
		if postpone, err = controller.finishBootloaderUpgrade(); err != nil {
			return false, controller.finishUpgrade(err)
		}
	}

	if postpone {
		controller.state.UpgradeState = upgradeTrySwitch

		if err = controller.saveState(); err != nil {
			return false, controller.finishUpgrade(nil)
		}

		if err = controller.systemReboot(); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, controller.finishUpgrade(nil)
}

// RevertFinished notifies state controller about finish of revert
func (controller *Controller) RevertFinished(version uint64, status error,
	moduleStatus map[string]error) (postpone bool, err error) {
	log.WithField("version", version).Debug("State controller upgrade finished")

	return false, errors.New("revert operation is not supported")
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *Controller) upgradeSecondFSPartition(id string, part partition.Info) (err error) {
	fsModule, err := controller.getFSModuleByID(id)
	if err != nil {
		return err
	}

	if err = fsModule.SetPartitionForUpdate(part.Device, part.Type); err != nil {
		return err
	}

	path, ok := controller.state.UpgradeModules[id]
	if !ok {
		return fmt.Errorf("module %s not found in state", id)
	}

	if err = fsModule.Upgrade(path); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) finishRootFSUpgrade() (postpone bool, err error) {
	if controller.state.UpgradeState == upgradeStarted {
		if err = controller.tryNewRootFS(); err != nil {
			return false, err
		}

		return true, nil
	}

	log.Warn("System reboot is scheduled")

	return false, nil
}

func (controller *Controller) finishBootloaderUpgrade() (postpone bool, err error) {
	if controller.state.UpgradeState == upgradeStarted {
		if err = controller.tryNewBootloader(); err != nil {
			return false, err
		}

		return true, nil
	}

	log.Warn("System reboot is scheduled")

	return false, nil
}

func (controller *Controller) waitSystemCheckFinished() (err error) {
	for !controller.systemStatus.checked {
		controller.systemStatus.cond.Wait()
	}

	return controller.systemStatus.status
}

func (controller *Controller) getRootFSUpdatePartition() (partition partition.Info) {
	updatePart := (controller.activeRootPart + 1) % len(controller.config.RootPartitions)

	return controller.rootPartInfo[updatePart]
}

func (controller *Controller) getBootloaderUpdatePartition() (partition partition.Info) {
	updatePart := (controller.activeBootPart + 1) % len(controller.config.BootPartitions)

	return controller.bootPartInfo[updatePart]
}

func (controller *Controller) initModules(modules map[string]string) (err error) {
	for id := range modules {
		switch id {
		case rootFSModuleID:
			if err := controller.initFileSystemUpdateModule(rootFSModuleID, controller.getRootFSUpdatePartition()); err != nil {
				return err
			}

		case bootloaderModuleID:
			if err := controller.initFileSystemUpdateModule(bootloaderModuleID, controller.getBootloaderUpdatePartition()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (controller *Controller) getFSModuleByID(id string) (module fsModule, err error) {
	genericModule, err := controller.moduleProvider.GetModuleByID(id)
	if err != nil {
		return nil, err
	}

	module, ok := genericModule.(fsModule)
	if !ok {
		return nil, fmt.Errorf("module %s doesn't implement required interface", id)
	}

	return module, nil
}

func (controller *Controller) initFileSystemUpdateModule(id string, part partition.Info) (err error) {
	log.Debug("Init module: ", id)

	fsModule, err := controller.getFSModuleByID(id)
	if err != nil {
		return err
	}

	if err = fsModule.SetPartitionForUpdate(part.Device, part.Type); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) updatePartInfo() (err error) {
	controller.bootPartInfo = make([]partition.Info, 0, len(controller.config.BootPartitions))

	for _, part := range controller.config.BootPartitions {
		info, err := partition.GetInfo(part)
		if err != nil {
			return err
		}

		controller.bootPartInfo = append(controller.bootPartInfo, info)
	}

	controller.bootEFIIds = make([]uint16, 0, len(controller.config.BootPartitions))

	for _, info := range controller.bootPartInfo {
		boot, err := controller.efiProvider.GetBootByPartUUID(info.PartUUID)
		if err != nil {
			return err
		}

		controller.bootEFIIds = append(controller.bootEFIIds, boot)
	}

	controller.rootPartInfo = make([]partition.Info, 0, len(controller.config.RootPartitions))

	for _, part := range controller.config.RootPartitions {
		info, err := partition.GetInfo(part)
		if err != nil {
			return err
		}

		controller.rootPartInfo = append(controller.rootPartInfo, info)
	}

	return nil
}

func (controller *Controller) parseBootCmd() (err error) {
	data, err := ioutil.ReadFile(controller.config.KernelCmdline)
	if err != nil {
		return err
	}

	controller.activeBootPart = -1
	controller.activeRootPart = -1
	controller.grubCurrentBootIndex = -1

	options := strings.Split(string(data), " ")

	for _, option := range options {
		option = strings.TrimSpace(option)

		switch {
		case strings.HasPrefix(option, kernelRootPrefix):
			partStr := strings.TrimPrefix(option, kernelRootPrefix)

			for i, partition := range controller.config.RootPartitions {
				if partition == partStr {
					controller.activeRootPart = i
					break
				}
			}

		case strings.HasPrefix(option, kernelVersionPrefix):
			var err error

			if controller.version, err = strconv.ParseUint(strings.TrimPrefix(option, kernelVersionPrefix), 10, 64); err != nil {
				return err
			}

		case strings.HasPrefix(option, kernelBootIndexPrefix):
			rootIndex, err := strconv.ParseInt(strings.TrimPrefix(option, kernelBootIndexPrefix), 10, 0)
			if err != nil {
				return err
			}

			controller.grubCurrentBootIndex = int(rootIndex)
		}
	}

	if controller.grubCurrentBootIndex < 0 {
		return errors.New("can't define grub boot index")
	}

	log.WithField("index", controller.grubCurrentBootIndex).Debug("GRUB boot index")

	if controller.activeRootPart < 0 {
		return errors.New("can't define active root FS")
	}

	log.WithField("partition",
		controller.rootPartInfo[controller.activeRootPart].Device).Debug("Active root partition")

	controller.activeBootPart, err = controller.getCurrentBootIndex()
	if err != nil {
		return err
	}

	log.WithField("partition",
		controller.bootPartInfo[controller.activeBootPart].Device).Debug("Active boot partition")

	return nil
}

func (controller *Controller) initState() (err error) {
	defer func() {
		if err != nil {
			log.Warnf("State file error: %s. Create new one.", err)

			// create new state file with default values if not exist or any other error occurs
			log.Debug("Create new state file")

			if err = os.MkdirAll(filepath.Dir(controller.config.StateFile), 0755); err != nil {
				return
			}

			if err = controller.saveState(); err != nil {
				return
			}
		}
	}()

	stateJSON, err := ioutil.ReadFile(controller.config.StateFile)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(stateJSON, &controller.state); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) saveState() (err error) {
	stateJSON, err := json.Marshal(&controller.state)
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(controller.config.StateFile, stateJSON, 0644); err != nil {
		return err
	}

	return nil
}

func isModuleUpgraded(id string, modules map[string]string) (result bool) {
	if modules == nil {
		return false
	}

	if _, ok := modules[id]; !ok {
		return false
	}

	return true
}

func (controller *Controller) startUpgrade(version uint64, modules map[string]string) (err error) {
	if isModuleUpgraded(rootFSModuleID, modules) {
		env, err := controller.newGrubEnv(controller.activeBootPart)
		if err != nil {
			return err
		}
		defer func() {
			if grubErr := env.close(); grubErr != nil {
				if err == nil {
					err = grubErr
				}
			}
		}()

		// Disable updating (fallback) partition during update. It is required in case of unexpected reboot,
		// GRUB will not boot from this partition.
		if err = controller.disableFallbackPartition(env); err != nil {
			return err
		}
	}

	if isModuleUpgraded(bootloaderModuleID, modules) {
		// Disable updating (fallback) bootloader during update. It is required in case of unexpected reboot,
		// system will not boot from this bootloader.

		if err = controller.disableFallbackBootloader(); err != nil {
			return err
		}
	}

	controller.state = controllerState{
		UpgradeState:   upgradeStarted,
		UpgradeVersion: version,
		UpgradeModules: modules,
		GrubBootIndex:  controller.grubCurrentBootIndex,
		EFIBootIndex:   controller.activeBootPart,
	}

	if err = controller.saveState(); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) finishUpgrade(status error) (err error) {
	env, err := controller.newGrubEnv(controller.activeBootPart)
	if err != nil {
		return err
	}
	defer func() {
		if grubErr := env.close(); grubErr != nil {
			if err == nil {
				err = grubErr
			}
		}
	}()

	if status == nil {
		status = controller.updateBootOrder()

		if status == nil {
			controller.version = controller.state.UpgradeVersion

			log.WithField("newVersion", controller.version).Debugf("Finish upgrade successfully")

			status = controller.updateGrubEnv(env)
		}
	} else {
		log.Errorf("Finish upgrade, status: %s", status)
	}

	if err = controller.restoreFallbackPartition(env); err != nil {
		return err
	}

	controller.state.GrubBootIndex = controller.grubCurrentBootIndex
	controller.state.UpgradeState = upgradeFinished
	controller.state.UpgradeStatus = ""

	if status != nil {
		controller.state.UpgradeStatus = status.Error()
	}

	if err = controller.saveState(); err != nil {
		log.Errorf("Error finishing upgrade: %s", err)
	}

	return status
}

func (controller *Controller) updateGrubEnv(env *grubEnv) (err error) {
	log.Debug("Update GRUB environment")

	defaultBootIndex, err := env.getDefaultBootIndex()
	if err != nil {
		return err
	}

	if defaultBootIndex != controller.grubCurrentBootIndex {
		if err = env.setDefaultBootIndex(controller.grubCurrentBootIndex); err != nil {
			return err
		}

		controller.grubFallbackBootIndex = defaultBootIndex

		if err = env.setFallbackBootIndex(controller.grubFallbackBootIndex); err != nil {
			return err
		}
	}

	if err = env.setImageVersion(controller.state.UpgradeVersion); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) updateBootOrder() (err error) {
	activeBoot := controller.bootEFIIds[controller.activeBootPart]
	fallbackBoot := controller.bootEFIIds[(controller.activeBootPart+1)%len(controller.bootEFIIds)]

	if err = controller.efiProvider.SetBootOrder([]uint16{activeBoot, fallbackBoot}); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) tryNewRootFS() (err error) {
	log.Debug("Try switch to new root FS")

	env, err := controller.newGrubEnv(controller.activeBootPart)
	if err != nil {
		return err
	}
	defer func() {
		if grubErr := env.close(); grubErr != nil {
			if err == nil {
				err = grubErr
			}
		}
	}()

	if err = controller.enableFallbackPartition(env); err != nil {
		return err
	}

	if err = env.grub.SetVariable(grubSwitchVar, "1"); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) tryNewBootloader() (err error) {
	log.Debug("Try switch to new bootloader")

	nextBoot := controller.bootEFIIds[(controller.activeBootPart+1)%len(controller.bootEFIIds)]

	if err = controller.efiProvider.SetBootNext(nextBoot); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) enableFallbackPartition(env *grubEnv) (err error) {
	controller.grubFallbackBootIndex = (controller.grubCurrentBootIndex + 1) % 2

	if err = env.setFallbackBootIndex(controller.grubFallbackBootIndex); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) disableFallbackPartition(env *grubEnv) (err error) {
	controller.grubFallbackBootIndex = -1

	if err = env.setFallbackBootIndex(controller.grubFallbackBootIndex); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) disableFallbackBootloader() (err error) {
	bootOrder, err := controller.efiProvider.GetBootOrder()
	if err != nil {
		return err
	}

	i := 0

	for _, boot := range bootOrder {
		if controller.bootEFIIds[(controller.activeBootPart+1)%len(controller.bootEFIIds)] == boot {
			continue
		}

		bootOrder[i] = boot
		i++
	}

	bootOrder = bootOrder[:i]

	if err = controller.efiProvider.SetBootOrder(bootOrder); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) getCurrentBootIndex() (index int, err error) {
	current, err := controller.efiProvider.GetBootCurrent()
	if err != nil {
		return -1, err
	}

	for i, boot := range controller.bootEFIIds {
		if current == boot {
			return i, nil
		}
	}

	return -1, errors.New("can't get current boot index")
}

func (controller *Controller) systemReboot() (err error) {
	log.Info("System reboot")

	syscall.Sync()

	if controller.config.PerformReboot {
		if err = syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); err != nil {
			return err
		}
	}

	return nil
}
