package statecontroller

import (
	"errors"
	"sync"
	"syscall"

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
	grub *GrubController
	efi  *EfiController

	storage Storage

	wg sync.WaitGroup
}

// Storage provides interface to get/set system version
type Storage interface {
	GetSystemVersion() (version uint64, err error)
	SetSystemVersion(version uint64) (err error)
}

type readyLocker struct {
	sync.Mutex

	ready bool
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var (
	errNotReady   = errors.New("controller not ready")
	errOutOfRange = errors.New("index out of range")
	errNotFound   = errors.New("index not found")
)

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new state controller instance
func New(bootParts, rootParts []string, storage Storage,
	cmdLineFile string, efiProvider EfiProvider) (controller *Controller, err error) {
	log.Info("Create state constoller")

	controller = &Controller{storage: storage}

	if len(bootParts) < 2 {
		return nil, errors.New("num of boot partitions should be more than 1")
	}

	if controller.efi, err = newEfiController(bootParts, efiProvider, &controller.wg); err != nil {
		return nil, err
	}

	if len(rootParts) < 2 {
		return nil, errors.New("num of root partitions should be more than 1")
	}

	if controller.grub, err = newGrubController(rootParts, cmdLineFile, controller.efi, &controller.wg); err != nil {
		return nil, err
	}

	controller.wg.Add(1)
	go controller.waitForSuccessBoot()

	return controller, nil
}

// Close closes state controller instance
func (controller *Controller) Close() (closeErr error) {
	log.Info("Close state constoller")

	if err := controller.grub.close(); err != nil {
		if closeErr == nil {
			closeErr = err
		}
	}

	if err := controller.efi.close(); err != nil {
		if closeErr == nil {
			closeErr = err
		}
	}

	return closeErr
}

// GetGrubController returns GRUB controller
func (controller *Controller) GetGrubController() (grub *GrubController) {
	return controller.grub
}

// GetEfiController returns EFI controller
func (controller *Controller) GetEfiController() (efi *EfiController) {
	return controller.efi
}

// GetVersion returns current system version
func (controller *Controller) GetVersion() (version uint64, err error) {
	return controller.storage.GetSystemVersion()
}

// SetVersion sets current system version
func (controller *Controller) SetVersion(version uint64) (err error) {
	return controller.storage.SetSystemVersion(version)
}

// GetPlatformID returns platform ID
func (controller *Controller) GetPlatformID() (id string, err error) {
	return "Nuance-OTA", nil
}

// SystemReboot performs system reboot
func (controller *Controller) SystemReboot() (err error) {
	log.Info("System reboot")

	if err = controller.grub.close(); err != nil {
		log.Errorf("Can't close grub controller: %s", err)
	}

	if err = controller.efi.close(); err != nil {
		log.Errorf("Can't close efi controller: %s", err)
	}

	syscall.Sync()

	if err = syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); err != nil {
		return err
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (controller *Controller) waitForSuccessBoot() {
	defer func() {
		controller.wg.Done()
	}()

	// Determine success boot and notify controllers
}

func (locker *readyLocker) checkReadyAndLock() (err error) {
	locker.Lock()

	if !locker.ready {
		locker.Unlock()

		return errNotReady
	}

	return nil
}
