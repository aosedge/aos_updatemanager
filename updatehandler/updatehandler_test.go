// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2021 Renesas Electronics Corporation.
// Copyright (C) 2021 EPAM Systems, Inc.
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

package updatehandler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/image"
	log "github.com/sirupsen/logrus"

	"github.com/aosedge/aos_updatemanager/config"
	"github.com/aosedge/aos_updatemanager/umclient"
	"github.com/aosedge/aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	opInit    = "init"
	opPrepare = "prepare"
	opUpdate  = "update"
	opApply   = "apply"
	opRevert  = "revert"
	opReboot  = "reboot"
)

const versionExistMsg = "component already has required vendor version: "

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	sync.Mutex
	updateState []byte
	aosVersions map[string]uint64
}

type testModule struct {
	id             string
	vendorVersion  string
	rebootRequired bool
	status         error
}

type orderInfo struct {
	id string
	op string
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

var tmpDir string

var components map[string]*testModule

var cfg *config.Config

var order []orderInfo

var mutex sync.Mutex

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err = os.MkdirTemp("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	updatehandler.RegisterPlugin("testmodule",
		func(id string, configJSON json.RawMessage,
			storage updatehandler.ModuleStorage,
		) (module updatehandler.UpdateModule, err error) {
			if _, ok := components[id]; !ok {
				components[id] = &testModule{id: id}
			}

			return components[id], nil
		})

	cfg = &config.Config{
		DownloadDir: path.Join(tmpDir, "downloadDir"),
		UpdateModules: []config.ModuleConfig{
			{ID: "id1", Plugin: "testmodule"},
			{ID: "id2", Plugin: "testmodule"},
			{ID: "id3", Plugin: "testmodule"},
		},
	}

	server := &http.Server{Addr: ":9000", Handler: http.FileServer(http.Dir(tmpDir)), ReadHeaderTimeout: time.Second}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Errorf("Can't serve http server: %s", err)
		}
	}()

	ret := m.Run()

	if err := server.Shutdown(context.Background()); err != nil {
		log.Errorf("Can't shutdown server: %s", err)
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestUpdate(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled},
			{ID: "id2", Status: umclient.StatusInstalled},
			{ID: "id3", Status: umclient.StatusInstalled},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	infos[2].URL = "http://localhost:9000/" + path.Base(infos[2].URL)

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	newStatus.State = umclient.StateUpdated
	components["id2"].rebootRequired = true
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus,
		map[string][]string{"id1": {opUpdate}, "id2": {opUpdate, opReboot, opUpdate}, "id3": {opUpdate}}, nil)

	// Reboot

	handler.Close()

	components = make(map[string]*testModule)
	order = nil

	if handler, err = updatehandler.New(cfg, storage, storage); err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	testOperation(t, handler, handler.Registered, &newStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Apply

	finalStatus := umclient.Status{State: umclient.StateIdle}

	for _, info := range infos {
		finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalled,
		})
	}

	components["id3"].rebootRequired = true
	order = nil

	testOperation(t, handler, handler.ApplyUpdate, &finalStatus,
		map[string][]string{"id1": {opApply}, "id2": {opApply}, "id3": {opApply, opReboot, opApply}}, nil)
}

func TestPrepareFail(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled},
			{ID: "id2", Status: umclient.StatusInstalled},
			{ID: "id3", Status: umclient.StatusInstalled},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedErr := aoserrors.New("prepare error")
	failedComponent := components["id2"]
	failedComponent.status = failedErr

	newStatus := currentStatus

	for _, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = failedErr.Error()
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        status,
			Error:         errStr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = failedErr.Error()
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == failedComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusError,
				Error:         failedErr.Error(),
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus,
		map[string][]string{"id1": {opRevert}, "id2": {opRevert, opReboot, opRevert}, "id3": {opRevert}}, nil)
}

func TestUpdateFailed(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled},
			{ID: "id2", Status: umclient.StatusInstalled},
			{ID: "id3", Status: umclient.StatusInstalled},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	failedErr := aoserrors.New("update error")
	failedComponent := components["id2"]
	failedComponent.status = failedErr

	newStatus = currentStatus

	for _, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = failedErr.Error()
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        status,
			Error:         errStr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = failedErr.Error()
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == failedComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusError,
				Error:         failedErr.Error(),
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus,
		map[string][]string{"id1": {opRevert}, "id2": {opRevert, opReboot, opRevert}, "id3": {opRevert}}, nil)
}

func TestUpdateSameVendorVersion(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 0},
			{ID: "id2", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 0},
			{ID: "id3", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 0},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	sameVersionComponent := components["id1"]
	sameVersionComponent.vendorVersion = "1.0"

	newStatus := currentStatus

	for i, info := range infos {
		if info.ID == sameVersionComponent.id {
			infos[i].VendorVersion = sameVersionComponent.vendorVersion

			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            sameVersionComponent.id,
				AosVersion:    info.AosVersion,
				VendorVersion: sameVersionComponent.vendorVersion,
				Status:        umclient.StatusError,
				Error:         versionExistMsg + sameVersionComponent.vendorVersion,
			})
		} else {
			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusInstalling,
			})
		}
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = versionExistMsg + sameVersionComponent.vendorVersion
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	newStatus = currentStatus
	newStatus.State = umclient.StateIdle
	order = nil

	for i, stateComponent := range newStatus.Components {
		if stateComponent.ID == sameVersionComponent.id {
			newStatus.Components[i].VendorVersion = sameVersionComponent.vendorVersion
		}
	}

	newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
		ID:            sameVersionComponent.id,
		AosVersion:    1,
		VendorVersion: sameVersionComponent.vendorVersion,
		Status:        umclient.StatusError,
		Error:         versionExistMsg + sameVersionComponent.vendorVersion,
	})

	testOperation(t, handler, handler.RevertUpdate, &newStatus, nil, nil)
}

func TestUpdateSameAosVersion(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 2},
			{ID: "id2", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 2},
			{ID: "id3", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 2},
		},
	}

	for _, element := range currentStatus.Components {
		if err := storage.SetAosVersion(element.ID, element.AosVersion); err != nil {
			t.Errorf("Can't set Aos version: %s", err)
		}
	}

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	errorComponent := umclient.ComponentStatusInfo{
		ID:            components["id2"].id,
		AosVersion:    2,
		VendorVersion: "",
		Status:        umclient.StatusError,
		Error:         "component already has required Aos version: 2",
	}

	newStatus := currentStatus
	newStatus.Components = append(newStatus.Components, errorComponent)
	newStatus.State = umclient.StateFailed
	newStatus.Error = "component already has required Aos version: 2"
	order = nil

	for i, info := range infos {
		if info.ID == errorComponent.ID {
			infos[i].AosVersion = errorComponent.AosVersion
		} else {
			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusInstalling,
			})
		}
	}

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	newStatus = currentStatus
	newStatus.State = umclient.StateIdle
	newStatus.Components = append(newStatus.Components, errorComponent)
	order = nil

	testOperation(t, handler, handler.RevertUpdate, &newStatus, nil, nil)
}

func TestUpdateWrongVersion(t *testing.T) {
	// test lower Aos version
	components = make(map[string]*testModule)
	storage := newTestStorage()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 3},
			{ID: "id2", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 3},
			{ID: "id3", Status: umclient.StatusInstalled, VendorVersion: "", AosVersion: 3},
		},
	}

	for _, element := range currentStatus.Components {
		if err := storage.SetAosVersion(element.ID, element.AosVersion); err != nil {
			t.Errorf("Can't set Aos version: %s", err)
		}
	}

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	lowerVersionComponent := components["id3"]

	newStatus := currentStatus

	for i, info := range infos {
		if info.ID == lowerVersionComponent.id {
			infos[i].AosVersion = info.AosVersion - 2

			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    infos[i].AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusError,
				Error:         "wrong Aos version",
			})
		} else {
			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusInstalling,
			})
		}
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = "wrong Aos version"
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == lowerVersionComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusError,
				Error:         "wrong Aos version",
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus, nil, nil)
}

func TestUpdateBadImage(t *testing.T) {
	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled},
			{ID: "id2", Status: umclient.StatusInstalled},
			{ID: "id3", Status: umclient.StatusInstalled},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedErrStr := "checksum sha512 mismatch"
	failedComponent := components["id2"]

	newStatus := currentStatus

	for i, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = failedErrStr
			infos[i].Sha512 = []byte{1, 2, 3, 4, 5, 6}
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        status,
			Error:         errStr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = failedErrStr
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	// Revert

	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == failedComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusError,
				Error:         failedErrStr,
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus,
		map[string][]string{"id1": {opRevert}, "id2": {opRevert}, "id3": {opRevert}}, nil)
}

func TestUpdatePriority(t *testing.T) {
	cfg := &config.Config{
		DownloadDir: path.Join(tmpDir, "downloadDir"),
		UpdateModules: []config.ModuleConfig{
			{ID: "id1", Plugin: "testmodule", UpdatePriority: 3, RebootPriority: 1},
			{ID: "id2", Plugin: "testmodule", UpdatePriority: 2, RebootPriority: 2},
			{ID: "id3", Plugin: "testmodule", UpdatePriority: 1, RebootPriority: 3},
		},
	}

	components = make(map[string]*testModule)
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled},
			{ID: "id2", Status: umclient.StatusInstalled},
			{ID: "id3", Status: umclient.StatusInstalled},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus, nil,
		[]orderInfo{{"id1", opInit}, {"id2", opInit}, {"id3", opInit}})

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, "")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil,
		[]orderInfo{{"id1", opPrepare}, {"id2", opPrepare}, {"id3", opPrepare}})

	// Update

	for _, component := range components {
		component.rebootRequired = true
	}

	newStatus.State = umclient.StateUpdated
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil,
		[]orderInfo{
			{"id1", opUpdate},
			{"id2", opUpdate},
			{"id3", opUpdate},
			{"id3", opReboot},
			{"id2", opReboot},
			{"id1", opReboot},
			{"id1", opUpdate},
			{"id2", opUpdate},
			{"id3", opUpdate},
		})

	// Apply

	for _, component := range components {
		component.rebootRequired = true
	}

	finalStatus := umclient.Status{State: umclient.StateIdle}
	order = nil

	for _, info := range infos {
		finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalled,
		})
	}

	testOperation(t, handler, handler.ApplyUpdate, &finalStatus, nil,
		[]orderInfo{
			{"id1", opApply},
			{"id2", opApply},
			{"id3", opApply},
			{"id3", opReboot},
			{"id2", opReboot},
			{"id1", opReboot},
			{"id1", opApply},
			{"id2", opApply},
			{"id3", opApply},
		})
}

func TestVendorVersionInUpdate(t *testing.T) {
	components = map[string]*testModule{
		"id1": {id: "id1", vendorVersion: "1.0"},
		"id2": {id: "id2", vendorVersion: "1.0"},
		"id3": {id: "id3", vendorVersion: "1.0"},
	}
	storage := newTestStorage()
	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled, VendorVersion: "1.0"},
			{ID: "id2", Status: umclient.StatusInstalled, VendorVersion: "1.0"},
			{ID: "id3", Status: umclient.StatusInstalled, VendorVersion: "1.0"},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components[1:2], "2.0")
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id2": {opPrepare}}, nil)

	// Update

	newStatus.State = umclient.StateUpdated
	components["id2"].vendorVersion = "2.0"
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus,
		map[string][]string{"id2": {opUpdate}}, nil)

	// Reboot

	handler.Close()

	order = nil

	if handler, err = updatehandler.New(cfg, storage, storage); err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}
	defer handler.Close()

	testOperation(t, handler, handler.Registered, &newStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newTestStorage() (storage *testStorage) {
	return &testStorage{aosVersions: make(map[string]uint64)}
}

func (storage *testStorage) SetUpdateState(state []byte) (err error) {
	storage.Lock()
	defer storage.Unlock()

	storage.updateState = state

	return nil
}

func (storage *testStorage) GetUpdateState() (state []byte, err error) {
	storage.Lock()
	defer storage.Unlock()

	return storage.updateState, nil
}

func (storage *testStorage) SetAosVersion(id string, version uint64) (err error) {
	storage.Lock()
	defer storage.Unlock()

	storage.aosVersions[id] = version

	return nil
}

func (storage *testStorage) GetAosVersion(id string) (version uint64, err error) {
	storage.Lock()
	defer storage.Unlock()

	return storage.aosVersions[id], nil
}

func (storage *testStorage) GetModuleState(id string) (state []byte, err error) {
	storage.Lock()
	defer storage.Unlock()

	return []byte("valid"), nil
}

func (storage *testStorage) SetModuleState(id string, state []byte) (err error) {
	storage.Lock()
	defer storage.Unlock()

	return nil
}

func (module *testModule) GetID() (id string) {
	return module.id
}

func (module *testModule) Init() (err error) {
	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opInit})
	mutex.Unlock()

	return aoserrors.Wrap(err)
}

func (module *testModule) GetVendorVersion() (version string, err error) {
	return module.vendorVersion, nil
}

func (module *testModule) Prepare(imagePath string, vendorVersion string, annotations json.RawMessage) (err error) {
	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opPrepare})
	mutex.Unlock()

	return aoserrors.Wrap(err)
}

func (module *testModule) Update() (rebootRequired bool, err error) {
	rebootRequired = module.rebootRequired
	module.rebootRequired = false

	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opUpdate})
	mutex.Unlock()

	return rebootRequired, err
}

func (module *testModule) Apply() (rebootRequired bool, err error) {
	rebootRequired = module.rebootRequired
	module.rebootRequired = false

	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opApply})
	mutex.Unlock()

	return rebootRequired, err
}

func (module *testModule) Revert() (rebootRequired bool, err error) {
	rebootRequired = module.rebootRequired
	module.rebootRequired = false

	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opRevert})
	mutex.Unlock()

	return rebootRequired, err
}

func (module *testModule) Reboot() (err error) {
	err = module.status
	module.status = nil

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opReboot})
	mutex.Unlock()

	return aoserrors.Wrap(err)
}

func (module *testModule) Close() (err error) {
	err = module.status
	module.status = nil

	return aoserrors.Wrap(err)
}

func checkComponentOps(compOps map[string][]string) (err error) {
	mutex.Lock()
	defer mutex.Unlock()

	for id, ops := range compOps {
		var orderOps []string

		for _, item := range order {
			if item.id == id {
				orderOps = append(orderOps, item.op)
			}
		}

		if !reflect.DeepEqual(ops, orderOps) {
			return aoserrors.Errorf("wrong component %s ops: %v", id, orderOps)
		}
	}

	return nil
}

func waitForStatus(handler *updatehandler.Handler, expectedStatus *umclient.Status) (err error) {
	select {
	case <-time.After(5 * time.Second):
		return aoserrors.New("wait operation timeout")

	case currentStatus := <-handler.StatusChannel():
		if expectedStatus == nil {
			return nil
		}

		if currentStatus.State != expectedStatus.State {
			return aoserrors.Errorf("wrong current state: %s", currentStatus.State)
		}

		if !strings.Contains(currentStatus.Error, expectedStatus.Error) {
			return aoserrors.Errorf("wrong error value: %s", currentStatus.Error)
		}

		for _, expectedItem := range expectedStatus.Components {
			index := len(expectedStatus.Components)

			for i, currentItem := range currentStatus.Components {
				if currentItem.ID == expectedItem.ID &&
					currentItem.VendorVersion == expectedItem.VendorVersion &&
					currentItem.AosVersion == expectedItem.AosVersion &&
					currentItem.Status == expectedItem.Status &&
					strings.Contains(currentItem.Error, expectedItem.Error) {
					index = i
					break
				}
			}

			if index == len(expectedStatus.Components) {
				return aoserrors.Errorf("expected item not found: %v", expectedItem)
			}

			currentStatus.Components = append(currentStatus.Components[:index], currentStatus.Components[index+1:]...)
		}

		if len(currentStatus.Components) != 0 {
			return aoserrors.Errorf("unexpected item found: %v", currentStatus.Components[0])
		}

		return nil
	}
}

func createImage(imagePath string) (fileInfo image.FileInfo, err error) {
	if err := exec.Command("dd", "if=/dev/null", "of="+imagePath, "bs=1M", "count=8").Run(); err != nil {
		return fileInfo, aoserrors.Wrap(err)
	}

	if fileInfo, err = image.CreateFileInfo(context.Background(), imagePath); err != nil {
		return fileInfo, aoserrors.Wrap(err)
	}

	return fileInfo, nil
}

func createUpdateInfos(currentStatus []umclient.ComponentStatusInfo,
	vendorVersion string,
) (infos []umclient.ComponentUpdateInfo, err error) {
	for _, status := range currentStatus {
		imagePath := path.Join(tmpDir, fmt.Sprintf("testimage_%s.bin", status.ID))

		imageInfo, err := createImage(imagePath)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		info := umclient.ComponentUpdateInfo{
			ID:            status.ID,
			AosVersion:    status.AosVersion + 1,
			VendorVersion: vendorVersion,
			URL:           "file://" + imagePath,
			Sha256:        imageInfo.Sha256,
			Sha512:        imageInfo.Sha512,
			Size:          imageInfo.Size,
		}

		infos = append(infos, info)
	}

	return infos, nil
}

func testOperation(
	t *testing.T,
	handler *updatehandler.Handler,
	operation func(),
	expectedStatus *umclient.Status,
	expectedOps map[string][]string,
	expectedOrder []orderInfo,
) {
	t.Helper()

	operation()

	if err := waitForStatus(handler, expectedStatus); err != nil {
		t.Errorf("Wait for status failed: %s", err)
	}

	if expectedOps != nil {
		if err := checkComponentOps(expectedOps); err != nil {
			t.Errorf("Component operation error: %s", err)
		}
	}

	if expectedOrder != nil {
		if !reflect.DeepEqual(expectedOrder, order) {
			t.Errorf("Unexpected component order: %v", order)
		}
	}
}
