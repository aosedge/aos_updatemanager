package aoscontroller

import (
	"errors"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
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
	storage Storage
}

// Storage provides interface to get/set system version
type Storage interface {
	GetSystemVersion() (version uint64, err error)
	SetSystemVersion(version uint64) (err error)
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new platform controller
func New(storage updatehandler.StateStorage, modules []config.ModuleConfig) (controller updatehandler.PlatformController, err error) {
	log.Info("Create platform constoller")

	controller = &Controller{storage: storage}

	return controller, nil
}

// Close closes state controller instance
func (controller *Controller) Close() (closeErr error) {
	log.Info("Close state constoller")

	return nil
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
	return "Test Platform", nil
}

// SystemReboot performs system reboot
func (controller *Controller) SystemReboot() (err error) {
	log.Info("System reboot")

	return errors.New("not implemented")
}
