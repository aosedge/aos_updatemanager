package aoscontroller

import (
	"syscall"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller platform controller
type Controller struct {
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new platform controller
func New() (controller updatehandler.PlatformController, err error) {
	log.Info("Create platform constoller")

	controller = &Controller{}

	return controller, nil
}

// Close closes state controller instance
func (controller *Controller) Close() (closeErr error) {
	log.Info("Close state constoller")

	return nil
}

// SystemReboot performs system reboot
func (controller *Controller) SystemReboot() (err error) {
	log.Info("System reboot")

	syscall.Sync()

	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}
