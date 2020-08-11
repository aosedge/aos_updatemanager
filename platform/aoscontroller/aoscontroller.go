package aoscontroller

import (
	"io/ioutil"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/
const modelFileName = "/etc/aos/model_name.txt"

/*******************************************************************************
 * Types
 ******************************************************************************/

// Controller platform controller
type Controller struct {
	storage    Storage
	platformID string
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
func New(storage Storage, modelFile string) (controller updatehandler.PlatformController, err error) {
	log.Info("Create platform constoller")

	modelName := ""
	data, err := ioutil.ReadFile(modelFile)
	if err == nil {
		modelVersion := strings.Split(string(data), ";")
		if len(modelVersion) > 0 {
			modelName = modelVersion[0]
		} else {
			log.Error("No model name in ", modelFile)
		}
	} else {
		log.Error("Error read model name: ", err)
	}

	controller = &Controller{storage: storage, platformID: modelName}

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
	return controller.platformID, nil
}

// SystemReboot performs system reboot
func (controller *Controller) SystemReboot() (err error) {
	log.Info("System reboot")

	syscall.Sync()

	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}
