package statecontroller

import (
	"errors"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/grub"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type systemStatus struct {
	cond    *sync.Cond
	status  error
	checked bool
}

type grubInstance struct {
	*grub.Instance
}

/*******************************************************************************
 * Private
 ******************************************************************************/

// TODO: Implement invalid flag for root FS partition.
// Make partition invalid during upgrading, restoring etc.
// GRUB should not try to load invalid partition.

func (controller *Controller) systemCheck() {
	var err error

	log.Info("Start system check")

	if err = controller.waitSuccessPoint(); err != nil {
		goto finish
	}

	if err = controller.checkBootState(); err != nil {
		goto finish
	}

	if err = controller.checkUpgrade(); err != nil {
		goto finish
	}

	// TODO: Add system integrity check and recovery here

finish:

	if err != nil {
		log.Errorf("System check error: %s", err)
	} else {
		log.Info("System check success")
	}

	controller.Lock()
	controller.systemStatus.status = err
	controller.systemStatus.checked = true
	controller.Unlock()

	controller.systemStatus.cond.Broadcast()
}

// This function is blocked till we detect that system load successfully.
// Consider to implement wait timeout or watchdog system reset.
func (controller *Controller) waitSuccessPoint() (err error) {
	return nil
}

// This function checks current boot state (active partitions, grub env etc.)
// and compare it with expected state. When there is state mismatch it tries
// to perform appropriate recovery procedures.
func (controller *Controller) checkBootState() (err error) {
	log.Debug("Check boot state")

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

	envVars, err := env.grub.GetVariables()
	if err != nil {
		return err
	}

	if len(envVars) == 0 {
		if err = env.init(controller); err != nil {
			return err
		}
	}

	rootIndex, err := env.getBootIndex()
	if err != nil {
		return err
	}

	if controller.grubBootIndex != rootIndex && controller.state.UpgradeState == upgradeFinished {
		log.Warn("System started from inactive root FS partition. Make it active")

		// Check if this partition is ok and make it as active. Next integrity check will try to recover bad partition.
		// Could be situation when upgrade is in progress but state file corrupted and we are boot with new partition.
		// TODO: detect this situation.
		if err = env.grub.SetVariable(grubBootIndexVar, strconv.FormatInt(int64(controller.grubBootIndex), 10)); err != nil {
			return err
		}
	}

	return nil
}

// This function checks if we are in the middle of upgrade
func (controller *Controller) checkUpgrade() (err error) {
	if controller.state.UpgradeState == upgradeFinished {
		return nil
	}

	log.Debug("Check upgrade state")

	if controller.state.UpgradeState == upgradeTrySwitch {
		if controller.isModuleUpgraded(rootFSModuleID) {
			// New root FS boot failed, finish upgrade with failed status
			if controller.state.GrubBootIndex == controller.grubBootIndex {
				return controller.finishUpgrade(errors.New("new root FS boot failed"))
			}

			// New root FS is ok, finish upgrade with success status
			return controller.finishUpgrade(nil)
		}
	}

	return nil
}
