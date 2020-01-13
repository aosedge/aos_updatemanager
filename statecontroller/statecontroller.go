package statecontroller

import (
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller state controller instance
type Controller struct {
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new state controller instance
func New(configJSON []byte) (controller *Controller, err error) {
	log.Info("Create state constoller")

	controller = &Controller{}

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
	return "", nil
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
