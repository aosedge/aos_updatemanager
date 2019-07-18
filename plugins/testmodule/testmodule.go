package main

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// TestModule test module
type TestModule struct {
	id string
	sync.Mutex
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// NewModule creates test module instance
func NewModule(id string, configJSON []byte) (module updatehandler.Module, err error) {
	log.WithField("id", id).Info("Create test module")

	testModule := &TestModule{id: id}

	return testModule, nil
}

// Close closes test module
func (module *TestModule) Close() {
	log.WithField("id", module.id).Info("Close test module")
}

// GetID returns module ID
func (module *TestModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Upgrade upgrade module
func (module *TestModule) Upgrade(fileName string) (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": fileName}).Info("Upgrade")

	return nil
}

// Revert revert module
func (module *TestModule) Revert() (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithField("id", module.id).Info("Revert")

	return nil
}
