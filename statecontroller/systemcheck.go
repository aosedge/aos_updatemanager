package statecontroller

import (
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

func (controller *Controller) systemCheck() {
	var err error

	log.Info("Start system check")

	if err = controller.waitSuccessPoint(); err != nil {
		goto finish
	}

	if err = controller.checkBootState(); err != nil {
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

	if controller.grubBootIndex != rootIndex {
		if !(controller.state.UpgradeInProgress && controller.state.SwitchRootFS) {
			log.Warn("System started from inactive root FS partition. Make it active")

			// Make current partition as active. Next integrity check will try to recover bad partition.
			if err = env.grub.SetVariable(grubBootIndexVar, strconv.FormatInt(int64(controller.grubBootIndex), 10)); err != nil {
				return err
			}
		}
	}

	return nil
}
