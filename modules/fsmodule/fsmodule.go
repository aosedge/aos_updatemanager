package fsmodule

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/partition"
)

//
// The sequence diagram of upgrade:
//
// * Init()                               module initialization
//
// * Upgrade() (rebootRequired = true)    upgrade second partition, mark it as
//                                        inactive (to do not select for normal
//                                        boot), set it as next boot and request
//                                        reboot
//
//------------------------------- Reboot ---------------------------------------
//
// * Upgrade() (rebootRequired = false)   check status after reboot, switch
//                                        to upgraded partition (make it active
//                                        and put at first place in boot order),
//                                        make second partition as inactive to
//                                        do not select for normal boot till
//                                        upgrade is finished
//
// * FinishUpgrade()                      return status and start same upgrade
//                                        on other partition, if it fails try to
//                                        copy content from active partition
//
//------------------------------------------------------------------------------
//
// CancelUpgrade() cancels upgrade and return the system to the previous state.
// It requests the reboot if the system is booted from the upgraded partition:
//
// * CancelUpgrade() (rebootRequired = true)     switch back to the previous
//                                               partition (make it active and
//                                               restore boot order), mark
//                                               second partition as inactive
//
//------------------------------- Reboot ---------------------------------------
//
// * CancelUpgrade() (rebootRequired = false)    return status and start recover
//                                               the second partition
//

/*******************************************************************************
 * Constants
 ******************************************************************************/

const metaDataFilename = "metadata.json"

const tmpMountpoint = "/tmp/aos/mountpoint"

const (
	ostreeRepoFolder = ".ostree_repo"
	ostreeBranchName = "nuance_ota"
)

const (
	incrementalType = "incremental"
	fullType        = "full"
)

//
// State machine:
//
// idleState            -> Upgrade() (rebootRequired = true)  -> waitForUpgradeReboot
// waitForUpgradeReboot -> reboot                             -> waitForSecondUpgrade
// waitForSecondUpgrade -> Upgrade() (rebootRequired = false) -> waitForFinish
// waitForFinish        -> FinishUpgrade()                    -> idleState
//

const (
	idleState = iota
	waitForUpgradeReboot
	waitForSecondUpgrade
	waitForFinish
	waitForCancelReboot
	waitForSecondCancel
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// FSModule fs upgrade module
type FSModule struct {
	sync.Mutex
	id string

	storage          Storage
	controller       StateController
	partInfo         []partition.Info
	currentPartition int
	state            moduleState
	wg               sync.WaitGroup
	// indicated some serious system error occurs and further upgrade is impossible
	systemError error
}

// Metadata upgrade metadata
type Metadata struct {
	ComponentType string `json:"componentType"`
	Version       int    `json:"version"`
	Description   string `json:"description,omitempty"`
	Type          string `json:"type"`
	Commit        string `json:"commit,omitempty"`
	Resources     string `json:"resources"`
}

// StateController state controller interface
type StateController interface {
	WaitForReady() (err error)
	GetCurrentBoot() (index int, err error)
	SetBootActive(index int, active bool) (err error)
	GetBootActive(index int) (active bool, err error)
	GetBootOrder() (bootOrder []int, err error)
	SetBootOrder(bootOrder []int) (err error)
	SetBootNext(index int) (err error)
	ClearBootNext() (err error)
}

// Storage storage interface
type Storage interface {
	GetModuleState(id string) (state []byte, err error)
	SetModuleState(id string, state []byte) (err error)
}

type moduleConfig struct {
	Partitions []string `json:"partitions"`
}

type moduleState struct {
	State            upgradeState `json:"state"`
	UpgradeVersion   uint64       `json:"upgradeVersion"`
	UpgradePartition int          `json:"upgradeIndex"`
	Metadata         Metadata
}

type upgradeState int

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates fs update module instance
func New(id string, controller StateController, storage Storage, configJSON []byte) (module *FSModule, err error) {
	log.Infof("Create %s module", id)

	module = &FSModule{id: id, controller: controller, storage: storage}

	var config moduleConfig

	if err = json.Unmarshal(configJSON, &config); err != nil {
		return nil, err
	}

	if err = module.updatePartInfo(config.Partitions); err != nil {
		return nil, err
	}

	if module.currentPartition, err = module.controller.GetCurrentBoot(); err != nil {
		return nil, err
	}

	if module.getState(); err != nil {
		return nil, err
	}

	// Prevent using upgrade API without Init
	module.systemError = errors.New("module is not initialized")

	return module, nil
}

// Close closes fs update module
func (module *FSModule) Close() (err error) {
	module.Lock()
	defer module.Unlock()

	log.Infof("Close %s module", module.id)

	return nil
}

// GetID returns module ID
func (module *FSModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Init initializes module
func (module *FSModule) Init() (err error) {
	module.Lock()
	defer module.Unlock()

	defer func() {
		module.systemError = err
	}()

	log.Infof("Initialize %s module", module.id)

	bootOrder, err := module.controller.GetBootOrder()
	if err != nil {
		return err
	}

	active0, err := module.controller.GetBootActive(0)
	if err != nil {
		return err
	}

	active1, err := module.controller.GetBootActive(1)
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{"currentBoot": module.currentPartition}).Debugf("%s: current boot", module.id)
	log.WithFields(log.Fields{"bootOrder": fmt.Sprintf("%d %d", bootOrder[0], bootOrder[1])}).Debugf("%s: boot order", module.id)
	log.WithFields(log.Fields{"active": fmt.Sprintf("%v %v", active0, active1)}).Debugf("%s: active boot", module.id)

	if err = module.controller.WaitForReady(); err != nil {
		return err
	}

	if module.state.State != idleState {
		if err = module.handleUpgradeReboot(); err != nil {
			return err
		}

		return nil
	}

	// If second partition is inactive, restore it
	active, err := module.controller.GetBootActive((module.currentPartition + 1) % len(module.partInfo))
	if err != nil {
		return err
	}

	if !active {
		module.wg.Add(1)
		go module.restorePartition((module.currentPartition + 1) % len(module.partInfo))
	}

	// TODO: something wrong with default partition. Implement checking and recovery mechanism
	if module.currentPartition != bootOrder[0] {
		log.Warnf("%s: boot from fallback partition", module.id)
	}

	return nil
}

// Upgrade upgrades module
func (module *FSModule) Upgrade(version uint64, imagePath string) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	module.wg.Wait()

	log.WithFields(log.Fields{"version": version, "path": imagePath}).Infof("Upgrade %s module", module.id)

	if module.systemError != nil {
		return false, module.systemError
	}

	if module.state.State != idleState && module.state.UpgradeVersion != version {
		// Trying to cancel current upgrade
		log.WithFields(log.Fields{"version": version}).Errorf("%s: another upgrade is in progress. Canceling", module.id)

		if rebootRequired, err = module.cancelUpgrade(); err != nil {
			return rebootRequired, err
		}

		if rebootRequired {
			return true, nil
		}

		// wait for partition restoring and start new upgrade
		module.wg.Wait()
	}

	switch module.state.State {
	case idleState:
		return module.startUpgrade(version, imagePath)

	case waitForUpgradeReboot:
		return true, nil

	case waitForSecondUpgrade:
		return module.secondUpgrade()

	case waitForFinish:
		return module.checkForFinish()

	default:
		return false, errors.New("invalid state")
	}
}

// CancelUpgrade cancels upgrade
func (module *FSModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	module.wg.Wait()

	log.WithFields(log.Fields{"version": version}).Infof("Cancel upgrade %s module", module.id)

	if module.systemError != nil {
		return false, module.systemError
	}

	if module.state.UpgradeVersion != version {
		return false, errors.New("invalid version")
	}

	return module.cancelUpgrade()
}

// FinishUpgrade finishes upgrade
func (module *FSModule) FinishUpgrade(version uint64) (err error) {
	module.Lock()
	defer module.Unlock()

	module.wg.Wait()

	log.WithFields(log.Fields{"version": version}).Infof("Finish upgrade %s module", module.id)

	if module.systemError != nil {
		return module.systemError
	}

	if module.state.State != waitForFinish {
		return errors.New("invalid state")
	}

	if module.state.UpgradeVersion != version {
		return errors.New("invalid version")
	}

	if _, err = module.checkForFinish(); err != nil {
		return err
	}

	if err = module.setState(idleState); err != nil {
		log.Errorf("Can't set state: %s", err)

		module.systemError = err

		return err
	}

	module.wg.Add(1)
	go module.upgradeSecondPartition()

	return nil
}

// Revert reverts module
func (module *FSModule) Revert(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Revert %s module", module.id)

	return false, errors.New("revert operation is not supported")
}

// CancelRevert cancels revert module
func (module *FSModule) CancelRevert(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Cancel revert %s module", module.id)

	return false, errors.New("revert operation is not supported")
}

// FinishRevert finished revert module
func (module *FSModule) FinishRevert(version uint64) (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Finish revert %s module", module.id)

	return errors.New("revert operation is not supported")
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (state upgradeState) String() string {
	return [...]string{
		"Idle", "WaitForUpgradeReboot", "WaitForSecondUpgrade", "WaitForFinish",
		"WaitForCancelReboot", "WaitForSecondCancel"}[state]
}

func (module *FSModule) getState() (err error) {
	stateJSON, err := module.storage.GetModuleState(module.id)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(stateJSON, &module.state); err != nil {
		return err
	}

	return nil
}

func (module *FSModule) setState(state upgradeState) (err error) {
	log.WithFields(log.Fields{"state": state}).Debugf("%s: state changed", module.id)

	module.state.State = state

	stateJSON, err := json.Marshal(module.state)
	if err != nil {
		return err
	}

	if err = module.storage.SetModuleState(module.id, stateJSON); err != nil {
		return err
	}

	return nil
}

func (module *FSModule) startUpgrade(version uint64, imagePath string) (rebootRequired bool, err error) {
	jsonMetadata, err := ioutil.ReadFile(path.Join(imagePath, metaDataFilename))
	if err != nil {
		return false, err
	}

	if err = json.Unmarshal(jsonMetadata, &module.state.Metadata); err != nil {
		return false, err
	}

	if module.state.Metadata.ComponentType != module.id {
		return false, fmt.Errorf("wrong componenet type: %s", module.state.Metadata.ComponentType)
	}

	module.state.Metadata.Resources = path.Join(imagePath, module.state.Metadata.Resources)
	module.state.UpgradeVersion = version
	module.state.UpgradePartition = (module.currentPartition + 1) % len(module.partInfo)

	if err = module.upgradePartition(module.state.UpgradePartition); err != nil {
		log.Errorf("%s: can't upgrade partition %s: %s", module.id, module.partInfo[module.state.UpgradePartition].Device, err)

		active, activeErr := module.controller.GetBootActive(module.state.UpgradePartition)
		if activeErr != nil || !active {
			module.wg.Add(1)
			go module.restorePartition(module.state.UpgradePartition)
		}

		return false, err
	}

	if err = module.setState(waitForUpgradeReboot); err != nil {
		return false, err
	}

	// Set next boot should be last upgrade operation in this state:
	// in case of any unexpected reboot system will boot into previous partition
	if err = module.controller.SetBootNext(module.state.UpgradePartition); err != nil {
		return false, err
	}

	return true, nil
}

func (module *FSModule) secondUpgrade() (rebootRequired bool, err error) {
	defer func() {
		if err != nil {
			if rebootRequired, module.systemError = module.restoreInitialState(idleState); module.systemError != nil {
				log.Errorf("Can't restore initial state: %s", module.systemError)
			}
		}
	}()

	if module.currentPartition != module.state.UpgradePartition {
		return false, errors.New("upgraded partition boot failed")
	}

	secondPartition := (module.currentPartition + 1) % len(module.partInfo)

	if err = module.controller.SetBootActive(module.currentPartition, true); err != nil {
		return false, err
	}

	if err = module.controller.SetBootOrder([]int{module.currentPartition, secondPartition}); err != nil {
		return false, err
	}

	// Make old as inactive to do not allow to boot from it till upgrade is finished
	if err = module.controller.SetBootActive(secondPartition, false); err != nil {
		return false, err
	}

	if err = module.setState(waitForFinish); err != nil {
		return false, err
	}

	return false, nil
}

func (module *FSModule) checkForFinish() (rebootRequired bool, err error) {
	if module.currentPartition != module.state.UpgradePartition {
		if rebootRequired, module.systemError = module.restoreInitialState(waitForUpgradeReboot); module.systemError != nil {
			log.Errorf("Can't restore initial state: %s", module.systemError)
		}
	}

	return rebootRequired, nil
}

func (module *FSModule) cancelUpgrade() (rebootRequired bool, err error) {
	switch module.state.State {
	case idleState:
		return false, nil

	case waitForUpgradeReboot:
		fallthrough
	case waitForSecondUpgrade:
		fallthrough
	case waitForFinish:
		return module.restoreInitialState(waitForCancelReboot)

	case waitForCancelReboot:
		return true, nil

	case waitForSecondCancel:
		if module.currentPartition == module.state.UpgradePartition {
			module.systemError = errors.New("initial partition boot failed")
			return false, module.systemError
		}

		return module.restoreInitialState(idleState)

	default:
		return false, errors.New("wrong state")
	}
}

func (module *FSModule) restoreInitialState(rebootState upgradeState) (rebootRequired bool, err error) {
	log.WithField("upgradePartition", module.state.UpgradePartition).Debugf("%s: restoring initial state", module.id)

	state := upgradeState(idleState)

	// Start restoring upgrade partition if we boot from initial one
	if module.currentPartition != module.state.UpgradePartition {
		module.wg.Add(1)
		go module.restorePartition(module.state.UpgradePartition)
	} else {
		rebootRequired = true
		state = rebootState
	}

	partIndex := (module.state.UpgradePartition + 1) % len(module.partInfo)

	if err = module.controller.ClearBootNext(); err != nil {
		return false, err
	}

	if err = module.controller.SetBootActive(partIndex, true); err != nil {
		return false, err
	}

	if err = module.controller.SetBootOrder([]int{partIndex, module.state.UpgradePartition}); err != nil {
		return false, err
	}

	if err = module.controller.SetBootActive(module.state.UpgradePartition, false); err != nil {
		return false, err
	}

	if err = module.setState(state); err != nil {
		return false, err
	}

	return rebootRequired, nil
}

func (module *FSModule) handleUpgradeReboot() (err error) {
	switch module.state.State {
	case waitForUpgradeReboot:
		if err = module.setState(waitForSecondUpgrade); err != nil {
			return err
		}

	case waitForCancelReboot:
		if err = module.setState(waitForSecondCancel); err != nil {
			return err
		}
	}

	return nil
}

func (module *FSModule) upgradePartition(partIndex int) (err error) {
	if module.state.Metadata.ComponentType != module.id {
		return fmt.Errorf("wrong componenet type: %s", module.state.Metadata.ComponentType)
	}

	if _, err = os.Stat(module.state.Metadata.Resources); err != nil {
		return err
	}

	switch module.state.Metadata.Type {
	case fullType:
		if err = module.fullUpgrade(partIndex); err != nil {
			return err
		}

	case incrementalType:
		if err = module.incrementalUpgrade(partIndex); err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unsupported upgrade type: %s", module.state.Metadata.Type)
	}

	return nil
}

func (module *FSModule) restorePartition(partIndex int) {
	var err error

	defer func() {
		if err != nil {
			log.Errorf("%s: can't restore partition %s: %s", module.id, module.partInfo[partIndex].Device, err)
			module.systemError = err
		}

		module.wg.Done()
	}()

	log.Debugf("%s: restoring partition %s", module.id, module.partInfo[partIndex].Device)

	if _, err = partition.Copy(module.partInfo[partIndex].Device,
		module.partInfo[(partIndex+1)%len(module.partInfo)].Device); err != nil {
		return
	}

	if err = module.controller.SetBootActive(partIndex, true); err != nil {
		return
	}
}

func (module *FSModule) upgradeSecondPartition() {
	partIndex := (module.state.UpgradePartition + 1) % len(module.partInfo)

	log.Debugf("%s: upgrading second partition %s", module.id, module.partInfo[partIndex].Device)

	if err := module.upgradePartition(partIndex); err != nil {
		log.Errorf("%s: second partition upgrade error: %s. Copying from current", module.id, err)

		module.restorePartition(partIndex)

		return
	}

	if module.systemError = module.controller.SetBootActive(partIndex, true); module.systemError != nil {
		log.Errorf("%s: can't restore partition %s: %s", module.id, module.partInfo[partIndex].Device, module.systemError)
	}

	module.wg.Done()
}

func (module *FSModule) updatePartInfo(partitions []string) (err error) {
	module.partInfo = make([]partition.Info, 0, len(partitions))

	for _, part := range partitions {
		info, err := partition.GetInfo(part)
		if err != nil {
			return err
		}

		module.partInfo = append(module.partInfo, info)
	}

	return nil
}

func (module *FSModule) fullUpgrade(partIndex int) (err error) {
	log.WithFields(log.Fields{
		"partition": module.partInfo[partIndex].Device,
		"from":      module.state.Metadata.Resources}).Debugf("Full %s upgrade", module.id)

	if err = module.controller.SetBootActive(partIndex, false); err != nil {
		return err
	}

	if _, err = partition.CopyFromArchive(module.partInfo[partIndex].Device, module.state.Metadata.Resources); err != nil {
		return err
	}

	return nil
}

func (module *FSModule) incrementalUpgrade(partIndex int) (err error) {
	log.WithFields(log.Fields{
		"partition": module.partInfo[partIndex].Device,
		"from":      module.state.Metadata.Resources,
		"commit":    module.state.Metadata.Commit}).Debugf("Incremental %s upgrade", module.id)

	if module.state.Metadata.Commit == "" {
		return fmt.Errorf("no commit field for incremental update")
	}

	if err = partition.Mount(module.partInfo[partIndex].Device,
		tmpMountpoint, module.partInfo[partIndex].Type); err != nil {
		return err
	}
	defer func() {
		if err := partition.Umount(tmpMountpoint); err != nil {
			log.Errorf("Can't unmount partitions: %s", err)
		}
	}()

	repoPath := path.Join(tmpMountpoint, ostreeRepoFolder)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return fmt.Errorf("ostree repo %s doesn't exist", repoPath)
	}

	if err = module.controller.SetBootActive(partIndex, false); err != nil {
		return err
	}

	log.Debugf("%s: apply static delta", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "static-delta", "apply-offline",
		module.state.Metadata.Resources).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	log.Debugf("%s: cleanup hard links", module.id)

	if err := removeRepoContent(tmpMountpoint); err != nil {
		return err
	}

	log.Debugf("%s: checkout to commit", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "checkout", module.state.Metadata.Commit,
		"-H", "-U", "--union", tmpMountpoint).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	log.Debugf("%s: cleanup repo", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--delete",
		ostreeBranchName).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--create="+ostreeBranchName,
		module.state.Metadata.Commit).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	if output, err := exec.Command("ostree", "--repo="+repoPath, "prune", ostreeBranchName, "--refs-only",
		"--depth=0").CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	return nil
}

func removeRepoContent(dir string) error {
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()

	names, err := dirFile.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		if name == ostreeRepoFolder {
			continue
		}

		if err = os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return err
		}

	}

	return nil
}
