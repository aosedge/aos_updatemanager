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
	"errors"
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

const (
	versionExistMsg = "component already has required version: "
	defaultVersion  = "1.0.0"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
	sync.Mutex
	updateState []byte
	versions    map[string]string
}

type testModule struct {
	id              string
	version         string
	preparedVersion string
	previousVersion string
	rebootRequired  bool
	status          error
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

var newVersion = "1.0.1"

var errComponentAlreadyHasDefaultVersion = errors.New(versionExistMsg + defaultVersion)

var errComponentAlreadyHasNewVersion = errors.New(versionExistMsg + newVersion)

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
				components[id] = &testModule{id: id, version: defaultVersion}
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	infos[2].URL = "http://localhost:9000/" + path.Base(infos[2].URL)

	newStatus := currentStatus

	for _, info := range infos {
		component := umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalling,
		}

		newStatus.Components = append(newStatus.Components, component)
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	newStatus.State = umclient.StateUpdated
	components["id2"].rebootRequired = true
	components["id2"].version = newVersion
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus,
		map[string][]string{"id1": {opUpdate}, "id2": {opUpdate, opReboot, opUpdate}, "id3": {opUpdate}}, nil)

	// Reboot

	handler.Close()

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
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalled,
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	for i := range infos {
		if infos[i].ID == "id2" {
			infos[i].Version = defaultVersion
		}
	}

	failedComponent := components["id2"]
	failedComponent.status = errComponentAlreadyHasDefaultVersion

	newStatus := currentStatus

	for _, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = versionExistMsg + defaultVersion
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  status,
			Error:   errStr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = versionExistMsg + defaultVersion
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == failedComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:      info.ID,
				Version: info.Version,
				Status:  umclient.StatusError,
				Error:   versionExistMsg + defaultVersion,
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus,
		map[string][]string{"id1": {opRevert}, "id2": {opRevert}, "id3": {opRevert}}, nil)
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	failedComponent := components["id2"]
	failedComponent.status = errComponentAlreadyHasNewVersion

	newStatus = currentStatus

	for _, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = versionExistMsg + newVersion
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  status,
			Error:   errStr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = versionExistMsg + newVersion
	order = nil

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus
	order = nil

	for _, info := range infos {
		if info.ID == failedComponent.id {
			finalStatus.Components = append(finalStatus.Components, umclient.ComponentStatusInfo{
				ID:      info.ID,
				Version: info.Version,
				Status:  umclient.StatusError,
				Error:   versionExistMsg + newVersion,
			})
		}
	}

	testOperation(t, handler, handler.RevertUpdate, &finalStatus,
		map[string][]string{"id1": {opRevert}, "id2": {opRevert, opReboot, opRevert}, "id3": {opRevert}}, nil)
}

func TestUpdateSameVersion(t *testing.T) {
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	newVersion := defaultVersion

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus
	expectedErr := versionExistMsg + newVersion

	for _, component := range currentStatus.Components {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      component.ID,
			Version: component.Version,
			Status:  umclient.StatusError,
			Error:   expectedErr,
		})
	}

	newStatus.State = umclient.StateFailed
	newStatus.Error = expectedErr
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedErrStr := "checksum sha256 mismatch"
	failedComponent := components["id2"]

	newStatus := currentStatus

	for i, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = failedErrStr
			infos[i].Sha256 = []byte{1, 2, 3, 4, 5, 6}
		}

		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  status,
			Error:   errStr,
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
				ID:      info.ID,
				Version: info.Version,
				Status:  umclient.StatusError,
				Error:   failedErrStr,
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
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus, nil,
		[]orderInfo{{"id1", opInit}, {"id2", opInit}, {"id3", opInit}})

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components, &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalling,
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
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalled,
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

func TestVersionInUpdate(t *testing.T) {
	components = map[string]*testModule{
		"id1": {id: "id1", version: defaultVersion},
		"id2": {id: "id2", version: defaultVersion},
		"id3": {id: "id3", version: defaultVersion},
	}
	storage := newTestStorage()

	for _, component := range components {
		_ = storage.SetVersion(component.id, component.version)
	}

	order = nil

	handler, err := updatehandler.New(cfg, storage, storage)
	if err != nil {
		t.Fatalf("Can't create update handler: %s", err)
	}

	currentStatus := umclient.Status{
		State: umclient.StateIdle,
		Components: []umclient.ComponentStatusInfo{
			{ID: "id1", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id2", Status: umclient.StatusInstalled, Version: defaultVersion},
			{ID: "id3", Status: umclient.StatusInstalled, Version: defaultVersion},
		},
	}

	testOperation(t, handler, handler.Registered, &currentStatus,
		map[string][]string{"id1": {opInit}, "id2": {opInit}, "id3": {opInit}}, nil)

	// Prepare

	infos, err := createUpdateInfos(currentStatus.Components[1:2], &newVersion)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	newStatus := currentStatus

	for _, info := range infos {
		newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
			ID:      info.ID,
			Version: info.Version,
			Status:  umclient.StatusInstalling,
		})
	}

	newStatus.State = umclient.StatePrepared
	order = nil

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id2": {opPrepare}}, nil)

	// Update

	newStatus.State = umclient.StateUpdated
	components["id2"].version = "2.0"
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
	return &testStorage{versions: make(map[string]string)}
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

func (storage *testStorage) SetVersion(id string, version string) (err error) {
	storage.Lock()
	defer storage.Unlock()

	storage.versions[id] = version

	return nil
}

func (storage *testStorage) GetVersion(id string) (version string, err error) {
	storage.Lock()
	defer storage.Unlock()

	return storage.versions[id], nil
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
	module.previousVersion = module.version

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opInit})
	mutex.Unlock()

	return aoserrors.Wrap(err)
}

func (module *testModule) GetVersion() (version string, err error) {
	return module.version, nil
}

func (module *testModule) Prepare(imagePath string, version string, annotations json.RawMessage) (err error) {
	err = module.status
	module.status = nil
	module.preparedVersion = version

	mutex.Lock()
	order = append(order, orderInfo{id: module.id, op: opPrepare})
	mutex.Unlock()

	return aoserrors.Wrap(err)
}

func (module *testModule) Update() (rebootRequired bool, err error) {
	rebootRequired = module.rebootRequired
	module.rebootRequired = false

	if module.preparedVersion != "" {
		module.previousVersion = module.version
		module.version = module.preparedVersion
		module.preparedVersion = ""
	}

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
	module.version = module.previousVersion

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
			return aoserrors.Errorf("wrong component %s ops: got=%v, expected=%v",
				id, orderOps, ops)
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

		log.WithField("status", currentStatus).Debug("Current status")

		if currentStatus.State != expectedStatus.State {
			return aoserrors.Errorf("wrong current state: %s", currentStatus.State)
		}

		if !strings.Contains(currentStatus.Error, expectedStatus.Error) {
			return aoserrors.Errorf("wrong error value: current=%s, expected=%s",
				currentStatus.Error, expectedStatus.Error)
		}

		for _, expectedItem := range expectedStatus.Components {
			index := len(currentStatus.Components)

			for i, currentItem := range currentStatus.Components {
				if currentItem.ID == expectedItem.ID &&
					currentItem.Version == expectedItem.Version &&
					currentItem.Status == expectedItem.Status &&
					strings.Contains(currentItem.Error, expectedItem.Error) {
					index = i
					break
				}
			}

			if index == len(currentStatus.Components) {
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
	version *string,
) (infos []umclient.ComponentUpdateInfo, err error) {
	for _, status := range currentStatus {
		imagePath := path.Join(tmpDir, fmt.Sprintf("testimage_%s_%s.bin", status.ID, status.Version))

		imageInfo, err := createImage(imagePath)
		if err != nil {
			return nil, aoserrors.Wrap(err)
		}

		info := umclient.ComponentUpdateInfo{
			ID:      status.ID,
			Version: status.Version,
			URL:     "file://" + imagePath,
			Sha256:  imageInfo.Sha256,
			Size:    imageInfo.Size,
		}

		if version != nil {
			info.Version = *version
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
