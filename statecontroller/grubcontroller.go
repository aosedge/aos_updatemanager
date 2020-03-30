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

// GrubController GRUB controller instance
type GrubController struct {
}

/*******************************************************************************
 * Interface
 ******************************************************************************/

// WaitForReady waits for controller to be ready
func (controller *GrubController) WaitForReady() (err error) {
	return nil
}

// GetCurrentBoot returns current boot index
func (controller *GrubController) GetCurrentBoot() (index int, err error) {
	return 0, nil
}

// SetBootActive sets boot item as active
func (controller *GrubController) SetBootActive(index int, active bool) (err error) {
	return nil
}

// GetBootActive returns boot item active state
func (controller *GrubController) GetBootActive(index int) (active bool, err error) {
	return false, nil
}

// GetBootOrder returns boot order
func (controller *GrubController) GetBootOrder() (indexes []int, err error) {
	return nil, nil
}

// SetBootOrder sets boot order
func (controller *GrubController) SetBootOrder(indexes []int) (err error) {
	return nil
}

// SetBootNext sets next boot item
func (controller *GrubController) SetBootNext(index int) (err error) {
	return nil
}

// ClearBootNext clears next boot item
func (controller *GrubController) ClearBootNext() (err error) {
	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newGrubController(cmdLineFile string) (controller *GrubController, err error) {
	log.Debug("Create GRUB controller")

	controller = &GrubController{}

	return controller, nil
}

func (controller *GrubController) close() (err error) {
	log.Debug("Close GRUB controller")

	return nil
}
