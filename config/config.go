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
	"path"
)

/*******************************************************************************
 * Types
 ******************************************************************************/
//Migration struct represents path for db migration
type Migration struct {
	MigrationPath       string `json:"migrationPath"`
	MergedMigrationPath string `json:"mergedMigrationPath"`
}

// Config instance
type Config struct {
	ServerURL     string         `json:"serverURL"`
	ID            string         `json:"id"`
	CACert        string         `json:"caCert"`
	CertStorage   string         `json:"certStorage"`
	WorkingDir    string         `json:"workingDir"`
	DownloadDir   string         `json:"downloadDir"`
	UpdateModules []ModuleConfig `json:"updateModules"`
	Migration     Migration      `json:"migration"`
}

// ModuleConfig module configuration
type ModuleConfig struct {
	ID             string `json:"id"`
	Plugin         string `json:"plugin"`
	Disabled       bool   `json:"disabled"`
	UpdatePriority uint32 `json:"updatePriority"`
	RebootPriority uint32 `json:"rebootPriority"`
	Params         json.RawMessage
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

	if config.Migration.MigrationPath == "" {
		config.Migration.MigrationPath = "/usr/share/aos/updatemanager/migration"
	}

	if config.Migration.MergedMigrationPath == "" {
		config.Migration.MergedMigrationPath = path.Join(config.WorkingDir, "mergedMigration")
	}

	return config, nil
}
