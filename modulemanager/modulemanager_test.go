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

package modulemanager_test

import (
	"errors"
	"os"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
	"aos_updatemanager/modulemanager"
	"aos_updatemanager/modulemanager/testmodule"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Types
 ******************************************************************************/

/*******************************************************************************
 * Vars
 ******************************************************************************/

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {

	modulemanager.Register("test", func(id string, configJSON []byte) (module interface{}, err error) {
		return testmodule.New(id, configJSON)
	})
	modulemanager.Register("wrongtest", func(id string, configJSON []byte) (module interface{}, err error) {
		return nil, errors.New("error")
	})

	ret := m.Run()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetModule(t *testing.T) {
	const numIds = 5

	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, numIds)}

	for i := 0; i < numIds; i++ {
		cfg.Modules = append(cfg.Modules, config.ModuleConfig{
			ID:     "id" + strconv.Itoa(i),
			Module: testmodule.Name})
	}

	moduleManager, err := modulemanager.New(&cfg)
	if err != nil {
		t.Fatalf("Can't create module manager: %s", err)
	}
	defer func() {
		if err := moduleManager.Close(); err != nil {
			t.Errorf("Can't close module manager: %s", err)
		}
	}()

	for i := 0; i < numIds; i++ {
		id := "id" + strconv.Itoa(i)

		module, err := moduleManager.GetModuleByID(id)

		if module == nil {
			t.Errorf("Module %s should not be nil", id)
		}

		if err != nil {
			t.Errorf("Can't get module %s: %s", id, err)
		}
	}

	//Check GetModuleById for wrong id
	module, err := moduleManager.GetModuleByID("id" + strconv.Itoa(numIds+2))
	if module != nil && err == nil {
		t.Error("Expecting error for nonexisting id")
	}
}

func TestNoModules(t *testing.T) {
	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, 0)}
	TryCreateModuleManager(&cfg, t)
}

func TestDisabledModules(t *testing.T) {
	numIds := 5
	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, numIds)}

	for i := 0; i < numIds; i++ {
		cfg.Modules = append(cfg.Modules, config.ModuleConfig{
			ID:       "id" + strconv.Itoa(i),
			Module:   testmodule.Name,
			Disabled: true})
	}

	TryCreateModuleManager(&cfg, t)
}

func TestNoFuncModule(t *testing.T) {
	numIds := 1
	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, numIds)}

	for i := 0; i < numIds; i++ {
		cfg.Modules = append(cfg.Modules, config.ModuleConfig{
			ID:     "id" + strconv.Itoa(i),
			Module: "unknownFunc"})
	}

	TryCreateModuleManager(&cfg, t)
}

func TestDuplicateModule(t *testing.T) {
	numIds := 5
	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, numIds)}

	for i := 0; i < numIds; i++ {
		cfg.Modules = append(cfg.Modules, config.ModuleConfig{
			ID:     "id" + strconv.Itoa(i),
			Module: testmodule.Name})
	}
	//NOTE: add duplicate module
	cfg.Modules = append(cfg.Modules, config.ModuleConfig{
		ID:     "id" + strconv.Itoa(1),
		Module: testmodule.Name})

	moduleManager, err := modulemanager.New(&cfg)
	if err != nil {
		t.Fatalf("Can't create module manager: %s", err)
	}
	defer func() {
		if err := moduleManager.Close(); err != nil {
			t.Errorf("Can't close module manager: %s", err)
		}
	}()
}

func TestErrorInFunc(t *testing.T) {
	numIds := 1
	cfg := config.Config{Modules: make([]config.ModuleConfig, 0, numIds)}

	for i := 0; i < numIds; i++ {
		cfg.Modules = append(cfg.Modules, config.ModuleConfig{
			ID:     "id" + strconv.Itoa(i),
			Module: "wrongtest"})
	}

	TryCreateModuleManager(&cfg, t)
}

/*******************************************************************************
 * Private funcs
 ******************************************************************************/

func TryCreateModuleManager(cfg *config.Config, t *testing.T) {
	moduleManager, err := modulemanager.New(cfg)
	if err == nil {
		if err := moduleManager.Close(); err != nil {
			t.Errorf("Can't close module manager: %s", err)
		}

		t.Fatal("Expecting error here")
	}
}
