package updatehandler

import (
	"errors"
	"plugin"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Module interface for module plugin
type Module interface {
	// Close closes module
	Close()
	// GetID returns module ID
	GetID() (id string)
	// Upgrade upgrade module
	Upgrade(fileName string) (err error)
	// Revert revert module
	Revert() (err error)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

// newModule creates new module instance
func newModule(id, pluginPath string, configJSON []byte) (module Module, err error) {
	plugin, err := plugin.Open(pluginPath)
	if err != nil {
		return module, err
	}

	newModuleSymbol, err := plugin.Lookup("NewModule")
	if err != nil {
		return module, err
	}

	newModuleFunction, ok := newModuleSymbol.(func(id string, configJSON []byte) (Module, error))
	if !ok {
		return module, errors.New("unexpected function type")
	}

	return newModuleFunction(id, configJSON)
}
