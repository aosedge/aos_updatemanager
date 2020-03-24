package fsmodule

import (
	"errors"
	"sync"

	log "github.com/sirupsen/logrus"
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
 * Types
 ******************************************************************************/

// FSModule fs upgrade module
type FSModule struct {
	sync.Mutex
	id string
}

/*******************************************************************************
 * Constants
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates fs update module instance
func New(id string, configJSON []byte) (module *FSModule, err error) {
	log.Infof("Create %s module", id)

	module = &FSModule{id: id}

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
