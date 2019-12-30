// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2019 Renesas Inc.
// Copyright 2019 EPAM Systems Inc.
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
	ServerURL   string
	Cert        string
	Key         string
	VersionFile string
	UpgradeDir  string
	WorkingDir  string
	Modules     []ModuleConfig
}

// ModuleConfig module configuration
type ModuleConfig struct {
	ID       string
	Disabled bool
	Module   string
	Params   interface{}
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
