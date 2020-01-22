package statecontroller

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shirou/gopsutil/disk"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const rootFSModuleID = "rootfs"

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller state controller instance
type Controller struct {
	moduleProvider ModuleProvider
	config         controllerConfig
}

//ConfigProvider interface to get configuration for update modules
type ConfigProvider interface {
	GetRootFsConfig() string
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
	RootPartitions []partitionInfo
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
	}

	if err = json.Unmarshal(configJSON, &controller.config); err != nil {
		return nil, err
	}

	if err = controller.initModules(); err != nil {
		return nil, err
	}

	return controller, nil
}

// Close closes state controller instance
func (controller *Controller) Close() (err error) {
	log.Info("Close state constoller")

	return nil
}

// GetVersion returns current installed image version
func (controller *Controller) GetVersion() (version uint64, err error) {
	return 0, nil
}

// GetPlatformID returns platform ID
func (controller *Controller) GetPlatformID() (id string, err error) {
	return "Nuance-OTA", nil
}

// Upgrade notifies state controller about start of system upgrade
func (controller *Controller) Upgrade(version uint64) (err error) {
	return nil
}

// Revert notifies state controller about start of system revert
func (controller *Controller) Revert(version uint64) (err error) {
	return nil
}

// UpgradeFinished notifies state controller about finish of upgrade
func (controller *Controller) UpgradeFinished(version uint64, moduleStatus map[string]error) (postpone bool, err error) {
	return false, nil
}

// RevertFinished notifies state controller about finish of revert
func (controller *Controller) RevertFinished(version uint64, moduleStatus map[string]error) (postpone bool, err error) {
	return false, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *Controller) getRootFSUpdatePartition() (partition partitionInfo, err error) {
	// We assume that active partition should be mounted at same time another root FS partition
	// should be unmounted. Return first unmounted partition from root FS partition list.

	stats, err := disk.Partitions(false)
	if err != nil {
		return partition, err
	}

	for _, partition = range controller.config.RootPartitions {
		found := false

		for _, stat := range stats {
			if stat.Device == partition.Device {
				found = true
				break
			}
		}

		if !found {
			return partition, nil
		}
	}

	return partition, errors.New("no root FS update partition found")
}

func (controller *Controller) initModules() (err error) {
	// init root FS module

	module, err := controller.moduleProvider.GetModuleByID(rootFSModuleID)
	if err != nil {
		return err
	}

	fsModule, ok := module.(fsModule)
	if !ok {
		return fmt.Errorf("module %s doesn't implement required interface", rootFSModuleID)
	}

	partition, err := controller.getRootFSUpdatePartition()
	if err != nil {
		return err
	}

	if err = fsModule.SetPartitionForUpdate(partition.Device, partition.FSType); err != nil {
		return err
	}

	return nil
}
