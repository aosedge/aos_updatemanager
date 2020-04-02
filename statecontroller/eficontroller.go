package statecontroller

import (
	"sync"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/efi"
	"aos_updatemanager/utils/partition"
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
	sync.Mutex

	efiProvider EfiProvider
	bootCurrent int
	bootIds     []uint16
	partInfo    []partition.Info
}

/*******************************************************************************
 * Interface
 ******************************************************************************/

// WaitForReady waits for controller is ready
func (controller *EfiController) WaitForReady() (err error) {
	controller.Lock()
	defer controller.Unlock()

	log.Debug("EFI controller: wait for ready")

	return nil
}

// GetCurrentBoot returns current boot index
func (controller *EfiController) GetCurrentBoot() (index int, err error) {
	controller.Lock()
	defer controller.Unlock()

	return controller.bootCurrent, nil
}

// SetBootActive sets boot item as active
func (controller *EfiController) SetBootActive(index int, active bool) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if index < 0 || index >= len(controller.bootIds) {
		return errOutOfRange
	}

	return controller.efiProvider.SetBootActive(controller.bootIds[index], active)
}

// GetBootActive returns boot item active state
func (controller *EfiController) GetBootActive(index int) (active bool, err error) {
	controller.Lock()
	defer controller.Unlock()

	if index < 0 || index >= len(controller.bootIds) {
		return false, errOutOfRange
	}

	return controller.efiProvider.GetBootActive(controller.bootIds[index])
}

// GetBootOrder returns boot order
func (controller *EfiController) GetBootOrder() (indexes []int, err error) {
	controller.Lock()
	defer controller.Unlock()

	indexes = make([]int, 0, len(controller.bootIds))

	bootOrder, err := controller.efiProvider.GetBootOrder()
	if err != nil {
		return nil, err
	}

	// Loop through boot order and put partition indexes into indexOrder array
	for _, orderID := range bootOrder {
		for i, bootID := range controller.bootIds {
			if orderID == bootID {
				indexes = append(indexes, i)
			}
		}
	}

	// We expect all partitions in the boot order
	if len(indexes) != len(controller.bootIds) {
		return nil, errNotFound
	}

	return indexes, nil
}

// SetBootOrder sets boot order
func (controller *EfiController) SetBootOrder(indexes []int) (err error) {
	controller.Lock()
	defer controller.Unlock()

	bootOrder, err := controller.efiProvider.GetBootOrder()
	if err != nil {
		return nil
	}

	orderIndex := 0

	// Remove partition indexes from boot order
	for _, orderID := range bootOrder {
		found := false

		for _, bootID := range controller.bootIds {
			if orderID == bootID {
				found = true
			}
		}

		if found {
			continue
		}

		bootOrder[orderIndex] = orderID
		orderIndex++
	}

	bootOrder = bootOrder[:orderIndex]

	newBootOrder := make([]uint16, len(indexes))

	for i, index := range indexes {
		if index < 0 || index >= len(controller.bootIds) {
			return errOutOfRange
		}

		newBootOrder[i] = controller.bootIds[index]
	}

	bootOrder = append(newBootOrder, bootOrder...)

	if err = controller.efiProvider.SetBootOrder(bootOrder); err != nil {
		return err
	}

	return nil
}

// SetBootNext sets next boot item
func (controller *EfiController) SetBootNext(index int) (err error) {
	controller.Lock()
	defer controller.Unlock()

	if index < 0 || index >= len(controller.bootIds) {
		return errOutOfRange
	}

	return controller.efiProvider.SetBootNext(controller.bootIds[index])
}

// ClearBootNext clears next boot item
func (controller *EfiController) ClearBootNext() (err error) {
	controller.Lock()
	defer controller.Unlock()

	return controller.efiProvider.DeleteBootNext()
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newEfiController(bootParts []string, efiProvider EfiProvider) (controller *EfiController, err error) {
	log.Debug("Create EFI controller")

	controller = &EfiController{efiProvider: efiProvider}

	if err = controller.init(bootParts); err != nil {
		return nil, err
	}

	return controller, nil
}

func (controller *EfiController) close() (err error) {
	log.Debug("Close EFI controller")

	if efiError := controller.efiProvider.Close(); efiError != nil {
		log.Errorf("Can't close efi provider: %s", err)
	}

	return nil
}

func (controller *EfiController) init(bootParts []string) (err error) {
	controller.bootIds = make([]uint16, 0, len(bootParts))
	controller.partInfo = make([]partition.Info, 0, len(bootParts))

	for _, part := range bootParts {
		info, err := partition.GetInfo(part)
		if err != nil {
			return err
		}

		bootID, err := controller.efiProvider.GetBootByPartUUID(info.PartUUID)
		if err != nil {
			log.Warnf("Can't find PARTUUID %s of %s in EFI bool list", info.PartUUID, info.Device)

			continue
		}

		log.WithFields(log.Fields{
			"device":   info.Device,
			"PARTUUID": info.PartUUID,
			"bootID":   bootID}).Debug("Boot partition")

		controller.bootIds = append(controller.bootIds, bootID)
		controller.partInfo = append(controller.partInfo, info)
	}

	if len(controller.bootIds) < len(bootParts) {
		log.Warnf("Not all boot partitions are in EFI boot list")
	}

	currentID, err := controller.efiProvider.GetBootCurrent()
	if err != nil {
		return err
	}

	for i, id := range controller.bootIds {
		if id == currentID {
			controller.bootCurrent = i

			return nil
		}
	}

	log.Warnf("Can't define current EFI boot item. Use default: %d", controller.bootCurrent)

	return nil
}

func (controller *EfiController) getCurrentBootPartition() (partInfo partition.Info, err error) {
	if controller.bootCurrent < 0 || controller.bootCurrent >= len(controller.partInfo) {
		return partition.Info{}, errOutOfRange
	}

	return controller.partInfo[controller.bootCurrent], nil
}

func (controller *EfiController) getNextBootPartition() (partInfo partition.Info, err error) {
	bootNext, err := controller.efiProvider.GetBootNext()
	if err != nil && err != efi.ErrNotFound {
		return partition.Info{}, err
	}

	if err == efi.ErrNotFound {
		return controller.getCurrentBootPartition()
	}

	for i, id := range controller.bootIds {
		if id == bootNext {
			return controller.partInfo[i], nil
		}
	}

	return partition.Info{}, errNotFound
}
