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

/*******************************************************************************
 * Types
 ******************************************************************************/

// FSModule fs upgrade module
type FSModule struct {
	sync.Mutex
	id string

	controller       StateController
	partInfo         []partition.Info
	currentPartition int
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
	GetCurrentBoot() (index int, err error)
}

type moduleConfig struct {
	Partitions []string `json:"partitions"`
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates fs update module instance
func New(id string, controller StateController, configJSON []byte) (module *FSModule, err error) {
	log.Infof("Create %s module", id)

	module = &FSModule{id: id, controller: controller}

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

	log.Infof("Initialize %s module", module.id)

	return nil
}

// Upgrade upgrades module
func (module *FSModule) Upgrade(version uint64, imagePath string) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version, "path": imagePath}).Infof("Upgrade %s module", module.id)

	jsonMetadata, err := ioutil.ReadFile(path.Join(imagePath, metaDataFilename))
	if err != nil {
		return false, err
	}

	var metadata Metadata

	if err = json.Unmarshal(jsonMetadata, &metadata); err != nil {
		return false, err
	}

	metadata.Resources = path.Join(imagePath, metadata.Resources)

	if err = module.upgradePartition((module.currentPartition+1)%len(module.partInfo), metadata); err != nil {
		return false, err
	}

	return false, nil
}

// CancelUpgrade cancels upgrade
func (module *FSModule) CancelUpgrade(version uint64) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Cancel upgrade %s module", module.id)

	return false, nil
}

// FinishUpgrade finishes upgrade
func (module *FSModule) FinishUpgrade(version uint64) (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Finish upgrade %s module", module.id)

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
func (module *FSModule) CancelRevert(rebootRequired bool, version uint64) (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{"version": version}).Infof("Cancel revert %s module", module.id)

	return errors.New("revert operation is not supported")
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

func (module *FSModule) upgradePartition(partIndex int, metadata Metadata) (err error) {
	if metadata.ComponentType != module.id {
		return fmt.Errorf("wrong componenet type: %s", metadata.ComponentType)
	}

	if _, err = os.Stat(metadata.Resources); err != nil {
		return err
	}

	switch metadata.Type {
	case fullType:
		if err = module.fullUpgrade(partIndex, metadata.Resources); err != nil {
			return err
		}

	case incrementalType:
		if err = module.incrementalUpgrade(partIndex, metadata.Resources, metadata.Commit); err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unsupported upgrade type: %s", metadata.Type)
	}

	return nil
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

func (module *FSModule) fullUpgrade(partIndex int, imagePath string) (err error) {
	log.WithFields(log.Fields{
		"partition": module.partInfo[partIndex].Device,
		"from":      imagePath}).Debugf("Full %s upgrade", module.id)

	if _, err = partition.CopyFromArchive(module.partInfo[partIndex].Device, imagePath); err != nil {
		return err
	}

	return nil
}

func (module *FSModule) incrementalUpgrade(partIndex int, imagePath string, commit string) (err error) {
	log.WithFields(log.Fields{
		"partition": module.partInfo[partIndex].Device,
		"from":      imagePath,
		"commit":    commit}).Debugf("Incremental %s upgrade", module.id)

	if commit == "" {
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

	log.Debugf("%s: apply static delta", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "static-delta", "apply-offline",
		imagePath).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	log.Debugf("%s: cleanup hard links", module.id)

	if err := removeRepoContent(tmpMountpoint); err != nil {
		return err
	}

	log.Debugf("%s: checkout to commit", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "checkout", commit,
		"-H", "-U", "--union", tmpMountpoint).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	log.Debugf("%s: cleanup repo", module.id)

	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--delete",
		ostreeBranchName).CombinedOutput(); err != nil {
		return fmt.Errorf("ostree error: %s, code: %s", string(output), err)
	}

	if output, err := exec.Command("ostree", "--repo="+repoPath, "refs", "--create="+ostreeBranchName,
		commit).CombinedOutput(); err != nil {
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
