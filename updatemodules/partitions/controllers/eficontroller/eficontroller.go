// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package eficontroller

import (
	"errors"
	"fmt"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/partition"
	log "github.com/sirupsen/logrus"

	"github.com/aosedge/aos_updatemanager/updatemodules/partitions/utils/efi"
)

/*******************************************************************************
 * Constants
 ******************************************************************************/

const defaultLoader = "/EFI/BOOT/bootx64.efi"

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller instance.
type Controller struct {
	efi       *efi.Instance
	loader    string
	bootItems []uint16
}

/*******************************************************************************
 * Variables
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new instance of EFI controller.
func New(partitions []string, loader string) (controller *Controller, err error) {
	log.Debug("Create EFI controller")

	controller = &Controller{loader: defaultLoader}

	if loader != "" {
		controller.loader = loader
	}

	if controller.efi, err = efi.New(); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	if err = controller.checkPartitions(partitions); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return controller, nil
}

// Close closes EFI controller.
func (controller *Controller) Close() {
	log.Debug("Close EFI controller")

	controller.efi.Close()
}

// GetCurrentBoot returns current boot part index.
func (controller *Controller) GetCurrentBoot() (index int, err error) {
	bootCurrent, err := controller.efi.GetBootCurrent()
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}

	for i, bootItem := range controller.bootItems {
		if bootCurrent == bootItem {
			return i, nil
		}
	}

	// if we boot from unknown entry, treat it as boot from part 0
	log.Warn("Boot from unknown partition")

	return 0, nil
}

// GetMainBoot returns boot main part index.
func (controller *Controller) GetMainBoot() (index int, err error) {
	bootOrder, err := controller.efi.GetBootOrder()
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}

	if len(bootOrder) == 0 {
		return 0, aoserrors.New("boot order is empty")
	}

	mainItem := bootOrder[0]

	for i, bootItem := range controller.bootItems {
		if mainItem == bootItem {
			return i, nil
		}
	}

	return 0, aoserrors.New("boot item not found")
}

// SetMainBoot sets boot main part index.
func (controller *Controller) SetMainBoot(index int) (err error) {
	if index >= len(controller.bootItems) {
		return aoserrors.New("wrong main boot index")
	}

	if err = controller.efi.SetBootNext(controller.bootItems[index]); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

// SetBootOK sets boot successful flag.
func (controller *Controller) SetBootOK() (err error) {
	bootOrder, err := controller.efi.GetBootOrder()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	bootCurrent, err := controller.efi.GetBootCurrent()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	found := false

	for _, bootItem := range controller.bootItems {
		if bootCurrent == bootItem {
			found = true

			break
		}
	}

	if !found {
		// if we boot from unknown entry, treat it as boot from part 0
		return nil
	}

	currentIndex := 0

	for i, orderItem := range bootOrder {
		if bootCurrent == orderItem {
			currentIndex = i

			break
		}
	}

	if currentIndex != 0 {
		bootOrder[0], bootOrder[currentIndex] = bootOrder[currentIndex], bootOrder[0]

		if err = controller.efi.SetBootOrder(bootOrder); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *Controller) checkPartitions(partitions []string) (err error) {
	for i, part := range partitions {
		info, err := partition.GetPartInfo(part)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		id, err := controller.efi.GetBootByPartUUID(info.PartUUID)
		if err != nil {
			if !errors.Is(err, efi.ErrNotFound) {
				return aoserrors.Wrap(err)
			}

			log.Warnf("Boot entry for partition %s not found. Creating...", part)

			if id, err = controller.efi.CreateBootEntry(1, part, controller.loader, fmt.Sprintf("Boot%d", i)); err != nil {
				return aoserrors.Wrap(err)
			}
		}

		controller.bootItems = append(controller.bootItems, id)
	}

	if err = controller.checkBootOrder(); err != nil {
		return aoserrors.Wrap(err)
	}

	return nil
}

func (controller *Controller) checkBootOrder() (err error) {
	bootOrder, err := controller.efi.GetBootOrder()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	// Divide boot array by module parttitions and other entries

	newBootOrder := make([]uint16, 0, len(bootOrder))
	partBootOrder := make([]uint16, 0, len(controller.bootItems))

	for _, orderItem := range bootOrder {
		found := false

		for _, bootItem := range controller.bootItems {
			if bootItem == orderItem {
				found = true

				continue
			}
		}

		if found {
			appendIfNotExist(&partBootOrder, orderItem)
		} else {
			appendIfNotExist(&newBootOrder, orderItem)
		}
	}

	// Check that all partitions are in boot order

	if len(controller.bootItems) > len(partBootOrder) {
		for _, bootItem := range controller.bootItems {
			present := false

			for _, orderItem := range partBootOrder {
				if bootItem == orderItem {
					present = true

					continue
				}
			}

			if !present {
				appendIfNotExist(&partBootOrder, bootItem)
			}
		}
	}

	newBootOrder = append(partBootOrder, newBootOrder...)

	// Update boot order if required

	updateBootOrder := false

	for i, newOrderItem := range newBootOrder {
		if newOrderItem != bootOrder[i] {
			updateBootOrder = true

			break
		}
	}

	if updateBootOrder {
		log.Warn("Boot order need to be updated")

		if err = controller.efi.SetBootOrder(newBootOrder); err != nil {
			return aoserrors.Wrap(err)
		}
	}

	return nil
}

func appendIfNotExist(slice *[]uint16, newItem uint16) {
	for _, item := range *slice {
		if newItem == item {
			return
		}
	}

	*slice = append(*slice, newItem)
}
