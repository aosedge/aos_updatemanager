// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2020 Renesas Inc.
// Copyright 2020 EPAM Systems Inc.
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

package boardconfigmodule

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const ioBufferSize = 1024 * 1024

const (
	newPostfix      = "_new"
	originalPostfix = "_orig"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// BoardCfgModule board configuration update module
type BoardCfgModule struct {
	id     string
	config moduleConfig
	sync.Mutex
}

type boardConfigVersion struct {
	VendorVersion string `json:"vendorVersion"`
}

type moduleConfig struct {
	Path string `json:"path"`
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates boardconfig module instance
func New(id string, configJSON json.RawMessage,
	storage updatehandler.StateStorage) (module updatehandler.UpdateModule, err error) {
	log.WithField("id", id).Info("Create boardconfig module")

	boardModule := &BoardCfgModule{id: id}

	if err = json.Unmarshal(configJSON, &boardModule.config); err != nil {
		return nil, err
	}

	return boardModule, nil
}

// Close closes boardconfig module
func (module *BoardCfgModule) Close() (err error) {
	log.WithField("id", module.id).Info("Close boardconfig module")
	return nil
}

// Init initializes module
func (module *BoardCfgModule) Init() (err error) {
	return nil
}

// GetID returns module ID
func (module *BoardCfgModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// GetVendorVersion returns vendor version
func (module *BoardCfgModule) GetVendorVersion() (version string, err error) {
	return module.getVendorVersionFromFile(module.config.Path)
}

// Update updates module
func (module *BoardCfgModule) Update(imagePath string, vendorVersion string, annotations json.RawMessage) (rebootRequired bool, err error) {
	module.Lock()
	defer module.Unlock()

	currentVersion, err := module.getVendorVersionFromFile(module.config.Path)
	if err == nil {
		if currentVersion == vendorVersion {
			log.Debug("Board configuration already up to date, version = ", vendorVersion)
			return false, nil
		}
	} else {
		log.Warn("Board configuration doesn't contain version, try to update")
	}

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": imagePath}).Info("Update")

	newBoardConfig := module.config.Path + newPostfix
	if err := extractFileFromGz(newBoardConfig, imagePath); err != nil {
		return false, err
	}

	newVersion, err := module.getVendorVersionFromFile(newBoardConfig)
	if err != nil {
		return false, err
	}

	if newVersion != vendorVersion {
		os.RemoveAll(newBoardConfig)
		return false, errors.New("vendorVersion missmatch")
	}

	// save original file
	if err := os.Rename(module.config.Path, module.config.Path+originalPostfix); err != nil {
		return false, err
	}

	if err := os.Rename(newBoardConfig, module.config.Path); err != nil {
		return false, err
	}

	return true, nil
}

// Cancel cancels update
func (module *BoardCfgModule) Cancel() (rebootRequired bool, err error) {
	if err := os.Rename(module.config.Path+originalPostfix, module.config.Path); err != nil {
		return false, err
	}

	return false, nil
}

// Finish finished update
func (module *BoardCfgModule) Finish() (err error) {
	os.RemoveAll(module.config.Path + originalPostfix)

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (module *BoardCfgModule) getVendorVersionFromFile(path string) (version string, err error) {
	boardFile := boardConfigVersion{}

	byteValue, err := ioutil.ReadFile(path)
	if err != nil {
		return version, err
	}

	if err = json.Unmarshal(byteValue, &boardFile); err != nil {
		return version, err
	}

	return boardFile.VendorVersion, nil
}

func extractFileFromGz(destination, source string) (err error) {
	log.WithFields(log.Fields{"src": destination, "dst": source}).Debug("Extract file from archive")

	srcFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return err
	}
	defer gz.Close()

	err = copyData(dstFile, gz)

	return err
}

func copyData(dst io.Writer, src io.Reader) (err error) {
	buf := make([]byte, ioBufferSize)

	for err != io.EOF {
		var readCount int

		if readCount, err = src.Read(buf); err != nil && err != io.EOF {
			return err
		}

		if readCount > 0 {
			if _, err = dst.Write(buf[:readCount]); err != nil {
				return err
			}
		}
	}

	return nil
}
