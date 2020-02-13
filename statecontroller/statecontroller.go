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
	grubVersionVar   = "NUANCE_VERSION"
	grubSwitchVar    = "NUANCE_TRY_SWITCH"
	grubBootIndexVar = "NUANCE_ACTIVE_BOOT_INDEX"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller state controller instance
type Controller struct {
	moduleProvider ModuleProvider
	config         controllerConfig
	state          controllerState
	version        uint64
	grubBootIndex  int
	activeRootPart int
	activeBootPart int
	systemStatus   systemStatus
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
}

type controllerState struct {
	UpgradeVersion    uint64
	UpgradeModules    []string
	SwitchRootFS      bool
	UpgradeInProgress bool
}

type fsModule interface {
	SetPartitionForUpdate(path, fsType string) (err error)
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

	if controller.state.UpgradeInProgress {
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
func (controller *Controller) Upgrade(version uint64, moduleIds []string) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if err = controller.waitSystemCheckFinished(); err != nil {
		return err
	}

	if err = controller.initModules(moduleIds); err != nil {
		return err
	}

	if controller.state.UpgradeInProgress {
		if controller.state.SwitchRootFS {
			// We can't perform upgrade on root FS switch. Disable root FS switch and perform system reboot
			// in order to be ready for new upgrades.
			log.Error("New upgrade can't be performed during root FS switch. Reboot is required.")

			if err = controller.finishUpgrade(); err != nil {
				log.Errorf("Can't finish upgrade: %s", err)
			}

			if err = controller.systemReboot(); err != nil {
				log.Errorf("Can't perform system reboot: %s", err)
			}

			return errors.New("new upgrade is impossible during root FS switch")
		}

		// We can perform new upgrade, just display the warning
		log.Warning("New upgrade request while another is in progress. Cancel current upgrade.")
	}

	if err = controller.startUpgrade(version, moduleIds); err != nil {
		return err
	}

	return nil
}

// Revert notifies state controller about start of system revert
func (controller *Controller) Revert(version uint64, moduleIds []string) (err error) {
	controller.Lock()
	defer controller.Unlock()

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

	if !controller.state.UpgradeInProgress {
		return false, errors.New("no upgrade was started")
	}

	if controller.state.UpgradeVersion != version {
		err = errors.New("upgrade version mistmatch")
		goto finishUpgrade
	}

	if status != nil {
		err = status
		goto finishUpgrade
	}

	switch {
	case controller.isModuleUpgraded(rootFSModuleID):
		if !controller.state.SwitchRootFS {
			if err = controller.tryRootFS(); err != nil {
				goto finishUpgrade
			}

			if err = controller.systemReboot(); err != nil {
				goto finishUpgrade
			}

			return true, nil
		}
	}

	if err = controller.updateGrubEnv(); err != nil {
		if controller.state.SwitchRootFS {
			if err := controller.systemReboot(); err != nil {
				log.Errorf("Can't perform system reboot: %s", err)
			}
		}
	}

finishUpgrade:

	if finishErr := controller.finishUpgrade(); finishErr != nil {
		log.Errorf("Error finishing upgrade: %s", err)

		if err == nil {
			err = finishErr
		}
	}

	return false, err
}

// RevertFinished notifies state controller about finish of revert
func (controller *Controller) RevertFinished(version uint64, status error,
	moduleStatus map[string]error) (postpone bool, err error) {
	return false, errors.New("revert operation is not supported")
}

/*******************************************************************************
 * Private
 ******************************************************************************/

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

func (controller *Controller) initModules(moduleIds []string) (err error) {
	for _, id := range moduleIds {
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

func (controller *Controller) initFileSystemUpdateModule(id string, part partitionInfo) (err error) {
	log.Debug("Init module: ", id)

	module, err := controller.moduleProvider.GetModuleByID(id)
	if err != nil {
		return err
	}

	fsModule, ok := module.(fsModule)
	if !ok {
		return fmt.Errorf("module %s doesn't implement required interface", id)
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
	controller.grubBootIndex = -1

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

			controller.grubBootIndex = int(rootIndex)
		}
	}

	if controller.grubBootIndex < 0 {
		return errors.New("can't define grub boot index")
	}

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
	stateJSON, err := ioutil.ReadFile(controller.config.StateFile)
	if os.IsNotExist(err) {
		// create new state file with default values if not exist
		log.Debug("Create new state file")

		if err = os.MkdirAll(filepath.Dir(controller.config.StateFile), 0755); err != nil {
			return err
		}

		if err = controller.saveState(); err != nil {
			return err
		}

		return nil
	}

	if err != nil {
		return err
	}

	if err = json.Unmarshal(stateJSON, &controller.state); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) saveState() (err error) {
	log.Debug("Save state file")

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

	for _, module := range controller.state.UpgradeModules {
		if module == id {
			return true
		}
	}

	return false
}

func (controller *Controller) startUpgrade(version uint64, moduleIds []string) (err error) {
	controller.state = controllerState{UpgradeVersion: version, UpgradeModules: moduleIds, UpgradeInProgress: true}

	if err = controller.saveState(); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) finishUpgrade() (err error) {
	controller.state.UpgradeInProgress = false
	controller.state.SwitchRootFS = false
	controller.state.UpgradeModules = nil

	if err = controller.saveState(); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) updateGrubEnv() (err error) {
	log.Debug("Update GRUB environment")

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

	if err = env.grub.SetVariable(grubVersionVar, strconv.FormatUint(controller.state.UpgradeVersion, 10)); err != nil {
		return err
	}

	if err = env.grub.SetVariable(grubBootIndexVar, strconv.FormatInt(int64(controller.grubBootIndex), 10)); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) tryRootFS() (err error) {
	log.Debug("Try switch root FS")

	controller.state.SwitchRootFS = true

	if err = controller.saveState(); err != nil {
		return err
	}

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

	if err = env.grub.SetVariable(grubSwitchVar, "1"); err != nil {
		return err
	}

	return nil
}

func (controller *Controller) systemReboot() (err error) {
	// TODO: implement proper system reboot.
	log.Info("System reboot")

	return nil
}
