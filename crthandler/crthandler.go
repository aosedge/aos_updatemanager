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

package crthandler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

/*******************************************************************************
 * Vars
 ******************************************************************************/

var plugins = make(map[string]NewPlugin)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Handler update handler
type Handler struct {
	sync.Mutex

	storage CrtStorage
	modules map[string]CrtModule
}

// CrtInfo certificate info
type CrtInfo struct {
	Issuer   string
	Serial   string
	CrtURL   string
	KeyURL   string
	NotAfter time.Time
}

// CrtStorage provides API to store/retreive certificates info
type CrtStorage interface {
	AddCertificate(crtType string, crt CrtInfo) (err error)
	GetCertificate(issuer, serial string) (crt CrtInfo, err error)
	GetCertificates(crtType string) (crts []CrtInfo, err error)
	RemoveCertificate(crtType, crtURL string) (err error)
}

// CrtModule provides API to manage module certificates
type CrtModule interface {
	SyncStorage() (err error)
	CreateKeys(systemID, password string) (csr string, err error)
	ApplyCertificate(crt string) (crtURL, keyURL string, err error)
	Close() (err error)
}

// NewPlugin plugin new function
type NewPlugin func(crtType string, configJSON json.RawMessage, storage CrtStorage) (module CrtModule, err error)

/*******************************************************************************
 * Public
 ******************************************************************************/

// RegisterPlugin registers module plugin
func RegisterPlugin(plugin string, newFunc NewPlugin) {
	log.WithField("plugin", plugin).Info("Register certificate plugin")

	plugins[plugin] = newFunc
}

// New returns pointer to new Handler
func New(cfg *config.Config, storage CrtStorage) (handler *Handler, err error) {
	handler = &Handler{modules: make(map[string]CrtModule), storage: storage}

	log.Debug("Create certificate handler")

	for _, moduleCfg := range cfg.CrtModules {
		if moduleCfg.Disabled {
			log.WithField("id", moduleCfg.ID).Debug("Skip disabled certificate module")

			continue
		}

		module, err := handler.createModule(moduleCfg.Plugin, moduleCfg.ID, moduleCfg.Params)
		if err != nil {
			return nil, err
		}

		handler.modules[moduleCfg.ID] = module
	}

	if err = handler.syncStorage(); err != nil {
		return nil, err
	}

	return handler, nil
}

// CreateKeys creates key pair
func (handler *Handler) CreateKeys(crtType, systemdID, password string) (csr string, err error) {
	handler.Lock()
	defer handler.Unlock()

	module, ok := handler.modules[crtType]
	if !ok {
		return "", fmt.Errorf("module %s not found", crtType)
	}

	return module.CreateKeys(systemdID, password)
}

// ApplyCertificate applies certificate
func (handler *Handler) ApplyCertificate(crtType string, cert string) (crtURL string, err error) {
	handler.Lock()
	defer handler.Unlock()

	module, ok := handler.modules[crtType]
	if !ok {
		return "", fmt.Errorf("module %s not found", crtType)
	}

	if crtURL, _, err = module.ApplyCertificate(cert); err != nil {
		return "", err
	}

	return crtURL, nil
}

// GetCertificate returns certificate info
func (handler *Handler) GetCertificate(crtType string, issuer []byte, serial string) (crtURL, keyURL string, err error) {
	handler.Lock()
	defer handler.Unlock()

	if serial == "" {
		crtInfos, err := handler.storage.GetCertificates(crtType)
		if err != nil {
			return "", "", err
		}

		var minTime time.Time

		var crtInfo CrtInfo

		for _, info := range crtInfos {
			if minTime.IsZero() || info.NotAfter.Before(minTime) {
				minTime = info.NotAfter
				crtInfo = info
			}
		}

		return crtInfo.CrtURL, crtInfo.KeyURL, nil
	}

	crtInfo, err := handler.storage.GetCertificate(base64.StdEncoding.EncodeToString(issuer), serial)
	if err != nil {
		return "", "", err
	}

	return crtInfo.CrtURL, crtInfo.KeyURL, nil
}

// Close closes certificate handler
func (handler *Handler) Close() {
	log.Debug("Close certificate handler")

	for _, module := range handler.modules {
		if err := module.Close(); err != nil {
			log.Errorf("Error closing module: %s", err)
		}
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (handler *Handler) createModule(plugin, crtType string, params json.RawMessage) (module CrtModule, err error) {
	newFunc, ok := plugins[plugin]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", plugin)
	}

	if module, err = newFunc(crtType, params, handler.storage); err != nil {
		return nil, err
	}

	return module, nil
}

func (handler *Handler) syncStorage() (err error) {
	handler.Lock()
	defer handler.Unlock()

	log.Debug("Sync certificate DB")

	for _, module := range handler.modules {
		if err = module.SyncStorage(); err != nil {
			return err
		}
	}

	return nil
}
