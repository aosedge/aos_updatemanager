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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
	"aos_updatemanager/database"
	"aos_updatemanager/modules/fsmodule"
	"aos_updatemanager/statecontroller"
	"aos_updatemanager/umserver"
	"aos_updatemanager/updatehandler"
	"aos_updatemanager/utils/efi"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const dbFileName = "updatemanager.db"

const (
	rootfsModuleID     = "rootfs"
	bootloaderModuleID = "bootloader"
)

const kernelCmdLine = "/proc/cmdline"

/*******************************************************************************
 * Vars
 ******************************************************************************/

// GitSummary provided by govvv at compile-time
var GitSummary string

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func cleanup(workingDir, dbFile string) {
	log.Debug("System cleanup")

	log.WithField("file", dbFile).Debug("Delete DB file")
	if err := os.RemoveAll(dbFile); err != nil {
		log.Fatalf("Can't cleanup database: %s", err)
	}
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func main() {
	// Initialize command line flags
	configFile := flag.String("c", "aos_updatemanager.cfg", "path to config file")
	strLogLevel := flag.String("v", "info", `log level: "debug", "info", "warn", "error", "fatal", "panic"`)

	flag.Parse()

	// Set log level
	logLevel, err := log.ParseLevel(*strLogLevel)
	if err != nil {
		log.Fatalf("Error: %s", err)
	}
	log.SetLevel(logLevel)

	log.WithFields(log.Fields{"configFile": *configFile, "version": GitSummary}).Info("Start update manager")

	cfg, err := config.New(*configFile)
	if err != nil {
		log.Fatalf("Can' open config file: %s", err)
	}

	// Create DB
	dbFile := path.Join(cfg.WorkingDir, dbFileName)

	db, err := database.New(dbFile)
	if err != nil {
		if err == database.ErrVersionMismatch {
			log.Warning("Unsupported database version")
			cleanup(cfg.WorkingDir, dbFile)
			db, err = database.New(dbFile)
		}

		if err != nil {
			log.Fatalf("Can't create database: %s", err)
		}
	}

	stateController, modules, err := createUpdateModules(cfg, db)
	if err != nil {
		log.Fatalf("Can't create update modules: %s", err)
	}
	defer func() {
		for _, module := range modules {
			module.Close()
		}

		stateController.Close()
	}()

	updater, err := updatehandler.New(cfg, modules, stateController, db)
	if err != nil {
		log.Fatalf("Can't create updater: %s", err)
	}
	defer updater.Close()

	server, err := umserver.New(cfg, updater)
	if err != nil {
		log.Fatalf("Can't create UM server: %s", err)
	}
	defer server.Close()

	// Handle SIGTERM
	terminateChannel := make(chan os.Signal, 1)
	signal.Notify(terminateChannel, os.Interrupt, syscall.SIGTERM)

	<-terminateChannel
}

func createUpdateModules(cfg *config.Config, db *database.Database) (stateController *statecontroller.Controller,
	modules []updatehandler.UpdateModule, err error) {

	// Create EFI provider

	efiProvider, err := efi.New()
	if err != nil {
		return nil, nil, err
	}

	// Get fs modules config to pass them to SC

	var (
		bootModuleCfg, rootModuleCfg fsmodule.ModuleConfig
	)

	for _, moduleCfg := range cfg.Modules {
		switch moduleCfg.ID {
		case rootfsModuleID:
			if err := json.Unmarshal(moduleCfg.Params, &rootModuleCfg); err != nil {
				return nil, nil, err
			}

		case bootloaderModuleID:
			if err := json.Unmarshal(moduleCfg.Params, &bootModuleCfg); err != nil {
				return nil, nil, err
			}
		}
	}

	// Create SC

	if stateController, err = statecontroller.New(
		bootModuleCfg.Partitions, rootModuleCfg.Partitions, db, kernelCmdLine,
		efiProvider); err != nil {
		return nil, nil, err
	}

	// Create modules

	modules = make([]updatehandler.UpdateModule, 0)

	for _, moduleCfg := range cfg.Modules {
		var module updatehandler.UpdateModule

		switch moduleCfg.ID {
		case rootfsModuleID:
			if module, err = fsmodule.New(rootfsModuleID, stateController.GetGrubController(),
				db, moduleCfg.Params); err != nil {
				return nil, nil, err
			}

		case bootloaderModuleID:
			if module, err = fsmodule.New(bootloaderModuleID, stateController.GetEfiController(),
				db, moduleCfg.Params); err != nil {
				return nil, nil, err
			}

		default:
			return nil, nil, fmt.Errorf("unknown module ID: %s", moduleCfg.ID)
		}

		modules = append(modules, module)
	}

	return stateController, modules, nil
}
