package statecontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	rootFSModuleID     = "rootfs"
	bootloaderModuleID = "bootloader"
)

const (
	kernelRootPrefix       = "root="
	kernelBootDevicePrefix = "NUANCE.bootDevice="
	kernelVersionPrefix    = "NUANCE.version="
	kernelBootIndexPrefix  = "NUANCE.bootIndex="
	kernelBootFormat       = "(hd%d,gpt%d)"
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
	config                controllerConfig
	state                 controllerState
	version               uint64
	grubCurrentBootIndex  int
	grubFallbackBootIndex int
	activeRootPart        int
	activeBootPart        int
	systemStatus          systemStatus
	sync.Mutex
}

// ModuleProvider module provider interface
type ModuleProvider interface {
	// GetModuleByID returns module by id
	GetModuleByID(id string) (module interface{}, err error)
}

type partitionInfo struct {
	Device string
	FSType string
}

type controllerConfig struct {
	KernelCmdline  string
	StateFile      string
	GRUBEnvFile    string
	RootPartitions []partitionInfo
	BootPartitions []partitionInfo
	PerformReboot  bool
}

type controllerState struct {
	UpgradeState   int
	UpgradeStatus  string
	UpgradeVersion uint64
	UpgradeModules map[string]string
	GrubBootIndex  int
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
func New(configJSON []byte, moduleProvider ModuleProvider) (controller *Controller, err error) {
	log.Info("Create state constoller")

	if moduleProvider == nil {
		return nil, errors.New("module provider should not be nil")
	}

	controller = &Controller{
		moduleProvider: moduleProvider,
		config: controllerConfig{
			GRUBEnvFile:   "EFI/BOOT/NUANCE/grubenv",
			KernelCmdline: "/proc/cmdline",
		},
	}

	controller.systemStatus.cond = sync.NewCond(controller)

	if err = json.Unmarshal(configJSON, &controller.config); err != nil {
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

	switch {
	case controller.isModuleUpgraded(rootFSModuleID):
		if postpone, err = controller.finishRootFSUpgrade(); err != nil {
			return false, controller.finishUpgrade(err)
		}
	}

	if postpone {
		controller.state.UpgradeState = upgradeTrySwitch

		if err = controller.saveState(); err != nil {
			return false, controller.finishUpgrade(nil)
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

func (controller *Controller) upgradeSecondFSPartition(id string, part partitionInfo) (err error) {
	fsModule, err := controller.getFSModuleByID(id)
	if err != nil {
		return err
	}

	if err = fsModule.SetPartitionForUpdate(part.Device, part.FSType); err != nil {
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

		if err = controller.systemReboot(); err != nil {
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

func (controller *Controller) getRootFSUpdatePartition() (partition partitionInfo) {
	updatePart := (controller.activeRootPart + 1) % len(controller.config.RootPartitions)

	return controller.config.RootPartitions[updatePart]
}

func (controller *Controller) getBootloaderUpdatePartition() (partition partitionInfo) {
	updatePart := (controller.activeBootPart + 1) % len(controller.config.BootPartitions)

	return controller.config.BootPartitions[updatePart]
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

func (controller *Controller) initFileSystemUpdateModule(id string, part partitionInfo) (err error) {
	log.Debug("Init module: ", id)

	fsModule, err := controller.getFSModuleByID(id)
	if err != nil {
		return err
	}

	if err = fsModule.SetPartitionForUpdate(part.Device, part.FSType); err != nil {
		return err
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
				if partition.Device == partStr {
					controller.activeRootPart = i
					break
				}
			}

		case strings.HasPrefix(option, kernelBootDevicePrefix):
			option = strings.TrimPrefix(option, kernelBootDevicePrefix)

			var (
				grubDisk int
				grubPart int
			)

			if _, err = fmt.Sscanf(option, kernelBootFormat, &grubDisk, &grubPart); err != nil {
				return err
			}

			for i, partition := range controller.config.BootPartitions {
				partStr := regexp.MustCompile("[[:digit:]]*$").FindString(partition.Device)
				configPart, err := strconv.Atoi(partStr)
				if err != nil {
					return err
				}

				if configPart == grubPart {
					controller.activeBootPart = i
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
		controller.config.RootPartitions[controller.activeRootPart].Device).Debug("Active root partition")

	if controller.activeBootPart < 0 {
		return errors.New("can't define active boot FS")
	}

	log.WithField("partition",
		controller.config.BootPartitions[controller.activeBootPart].Device).Debug("Active boot partition")

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

func (controller *Controller) isModuleUpgraded(id string) (result bool) {
	if controller.state.UpgradeModules == nil {
		return false
	}

	for module := range controller.state.UpgradeModules {
		if module == id {
			return true
		}
	}

	return false
}

func (controller *Controller) startUpgrade(version uint64, modules map[string]string) (err error) {
	_, updateRootFS := modules[rootFSModuleID]

	if updateRootFS {
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

	controller.state = controllerState{
		UpgradeState:   upgradeStarted,
		UpgradeVersion: version,
		UpgradeModules: modules,
		GrubBootIndex:  controller.grubCurrentBootIndex,
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
		controller.version = controller.state.UpgradeVersion

		log.WithField("newVersion", controller.version).Debugf("Finish upgrade successfully")

		status = controller.updateGrubEnv(env)
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
