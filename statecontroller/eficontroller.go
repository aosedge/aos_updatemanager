package statecontroller

import (
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// EfiProvider provides access to EFI vars
type EfiProvider interface {
	GetBootByPartUUID(partUUID uuid.UUID) (id uint16, err error)
	GetBootCurrent() (id uint16, err error)
	GetBootNext() (id uint16, err error)
	SetBootNext(id uint16) (err error)
	DeleteBootNext() (err error)
	GetBootOrder() (ids []uint16, err error)
	SetBootOrder(ids []uint16) (err error)
	GetBootActive(id uint16) (active bool, err error)
	SetBootActive(id uint16, active bool) (err error)
	Close() (err error)
}

// EfiController EFI controller instance
type EfiController struct {
	efiProvider EfiProvider
}

/*******************************************************************************
 * Interface
 ******************************************************************************/

// WaitForReady waits for controller is ready
func (controller *EfiController) WaitForReady() (err error) {
	return nil
}

// GetCurrentBoot returns current boot index
func (controller *EfiController) GetCurrentBoot() (index int, err error) {
	return 0, nil
}

// SetBootActive sets boot item as active
func (controller *EfiController) SetBootActive(index int, active bool) (err error) {
	return nil
}

// GetBootActive returns boot item active state
func (controller *EfiController) GetBootActive(index int) (active bool, err error) {
	return false, nil
}

// GetBootOrder returns boot order
func (controller *EfiController) GetBootOrder() (indexes []int, err error) {
	return nil, nil
}

// SetBootOrder sets boot order
func (controller *EfiController) SetBootOrder(indexes []int) (err error) {
	return nil
}

// SetBootNext sets next boot item
func (controller *EfiController) SetBootNext(index int) (err error) {
	return nil
}

// ClearBootNext clears next boot item
func (controller *EfiController) ClearBootNext() (err error) {
	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newEfiController(efiProvider EfiProvider) (controller *EfiController, err error) {
	log.Debug("Create EFI controller")

	controller = &EfiController{efiProvider: efiProvider}

	return controller, nil
}

func (controller *EfiController) close() (err error) {
	log.Debug("Close EFI controller")

	if efiError := controller.efiProvider.Close(); efiError != nil {
		log.Errorf("Can't close efi provider: %s", err)
	}

	return nil
}
