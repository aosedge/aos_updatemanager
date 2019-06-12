// Package config provides set of API to provide aos configuration
package config

import (
	"encoding/json"
	"os"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Config instance
type Config struct {
	ServerURL string
	Cert      string
	Key       string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new config object
func New(fileName string) (config *Config, err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return config, err
	}

	config = &Config{}

	decoder := json.NewDecoder(file)
	if err = decoder.Decode(config); err != nil {
		return config, err
	}

	return config, nil
}
