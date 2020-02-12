package statecontroller

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type systemStatus struct {
	cond    *sync.Cond
	status  error
	checked bool
}

/*******************************************************************************
 * Private
 ******************************************************************************/

// TODO: we should start system check and system recovery procedure at the beginning.
// * upgrade request should be blocked till this procedure is finished;
// * we should check if current rootIndex equals to NUANCE_ACTIVE_ROOT_INDEX in grub env variable.
//   If not and no upgrade in progress, it means that we try new root FS and something bad happens.
//   We have to reboot the system in this case.

func (controller *Controller) systemCheck() {
	var err error

	log.Info("Start system check")

	if err = controller.waitSuccessPoint(); err != nil {
		goto finish
	}

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
