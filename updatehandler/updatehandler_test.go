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

package updatehandler_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"reflect"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/image"

	"aos_updatemanager/config"
	"aos_updatemanager/umclient"
	"aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const waitStatusTimeout = 5 * time.Second

const (
	opInit    = "init"
	opPrepare = "prepare"
	opUpdate  = "update"
	opApply   = "apply"
	opRevert  = "revert"
	opReboot  = "reboot"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

type testStorage struct {
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
var imagePath string

var components = []*testModule{}

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
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	tmpDir, err = ioutil.TempDir("", "um_")
	if err != nil {
		log.Fatalf("Error create temporary dir: %s", err)
	}

	updatehandler.RegisterPlugin("testmodule",
		func(id string, configJSON json.RawMessage, storage updatehandler.ModuleStorage) (module updatehandler.UpdateModule, err error) {
			testModule := &testModule{id: id}

			components = append(components, testModule)

			return testModule, nil
		})

	cfg = &config.Config{
		ID:          "um",
		DownloadDir: path.Join(tmpDir, "downloadDir"),
		UpdateModules: []config.ModuleConfig{
			{ID: "id1", Plugin: "testmodule"},
			{ID: "id2", Plugin: "testmodule"},
			{ID: "id3", Plugin: "testmodule"}}}

	ret := m.Run()

	if err := os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestUpdate(t *testing.T) {
	go http.ListenAndServe(":9000", http.FileServer(http.Dir(tmpDir)))

	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	infos, err := createUpdateInfos(currentStatus.Components)
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

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	newStatus.State = umclient.StateUpdated
	components[1].rebootRequired = true

	testOperation(t, handler, handler.StartUpdate, &newStatus,
		map[string][]string{"id1": {opUpdate}, "id2": {opUpdate, opReboot, opUpdate}, "id3": {opUpdate}}, nil)

	// Reboot

	handler.Close()

	components = make([]*testModule, 0)

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

	components[2].rebootRequired = true

	testOperation(t, handler, handler.ApplyUpdate, &finalStatus,
		map[string][]string{"id1": {opApply}, "id2": {opApply}, "id3": {opApply, opReboot, opApply}}, nil)
}

func TestPrepareFail(t *testing.T) {
	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	infos, err := createUpdateInfos(currentStatus.Components)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedErr := errors.New("prepare error")
	failedComponent := components[1]
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

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus

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
	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	infos, err := createUpdateInfos(currentStatus.Components)
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

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus,
		map[string][]string{"id1": {opPrepare}, "id2": {opPrepare}, "id3": {opPrepare}}, nil)

	// Update

	failedErr := errors.New("update error")
	failedComponent := components[1]
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

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil, nil)

	// Revert

	failedComponent.rebootRequired = true
	finalStatus := currentStatus

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

func TestUpdateWrongVersion(t *testing.T) {
	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	// Test same vendor version

	infos, err := createUpdateInfos(currentStatus.Components)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	sameVersionComponent := components[0]
	sameVersionComponent.vendorVersion = "1.0"

	newStatus := currentStatus

	for i, info := range infos {
		if info.ID == sameVersionComponent.id {
			infos[i].VendorVersion = sameVersionComponent.vendorVersion
		} else {
			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusInstalling,
			})
		}
	}

	newStatus.State = umclient.StatePrepared

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	newStatus.State = umclient.StateUpdated

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil, nil)

	finalStatus := umclient.Status{State: umclient.StateIdle}

	for _, info := range infos {
		status := umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalled,
		}

		if sameVersionComponent.id == info.ID {
			status.AosVersion = info.AosVersion - 1
		}

		finalStatus.Components = append(finalStatus.Components, status)
	}

	testOperation(t, handler, handler.ApplyUpdate, &finalStatus, nil, nil)

	sameVersionComponent.vendorVersion = ""

	// Test same Aos version

	currentStatus = finalStatus

	if infos, err = createUpdateInfos(currentStatus.Components); err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	sameVersionComponent = components[1]

	newStatus = currentStatus

	for i, info := range infos {
		if info.ID == sameVersionComponent.id {
			infos[i].AosVersion = infos[i].AosVersion - 1
		} else {
			newStatus.Components = append(newStatus.Components, umclient.ComponentStatusInfo{
				ID:            info.ID,
				AosVersion:    info.AosVersion,
				VendorVersion: info.VendorVersion,
				Status:        umclient.StatusInstalling,
			})
		}
	}

	newStatus.State = umclient.StatePrepared

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	newStatus.State = umclient.StateUpdated

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil, nil)

	finalStatus = umclient.Status{State: umclient.StateIdle}

	for _, info := range infos {
		status := umclient.ComponentStatusInfo{
			ID:            info.ID,
			AosVersion:    info.AosVersion,
			VendorVersion: info.VendorVersion,
			Status:        umclient.StatusInstalled,
		}

		finalStatus.Components = append(finalStatus.Components, status)
	}

	testOperation(t, handler, handler.ApplyUpdate, &finalStatus, nil, nil)

	// Test lower Aos version

	currentStatus = finalStatus

	if infos, err = createUpdateInfos(currentStatus.Components); err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	lowerVersionComponent := components[2]

	newStatus = currentStatus

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

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	finalStatus = currentStatus

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
	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	infos, err := createUpdateInfos(currentStatus.Components)
	if err != nil {
		t.Fatalf("Can't create update infos: %s", err)
	}

	failedErr := errors.New("checksum sha512 mistmatch")
	failedComponent := components[1]

	newStatus := currentStatus

	for i, info := range infos {
		status := umclient.ComponentStatus(umclient.StatusInstalling)
		errStr := ""

		if info.ID == failedComponent.id {
			status = umclient.StatusError
			errStr = failedErr.Error()
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
	newStatus.Error = failedErr.Error()

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil, nil)

	// Revert

	finalStatus := currentStatus

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
		map[string][]string{"id1": {opRevert}, "id2": {opRevert}, "id3": {opRevert}}, nil)
}

func TestUpdatePriority(t *testing.T) {
	cfg := &config.Config{
		ID:          "um",
		DownloadDir: path.Join(tmpDir, "downloadDir"),
		UpdateModules: []config.ModuleConfig{
			{ID: "id1", Plugin: "testmodule", UpdatePriority: 3, RebootPriority: 1},
			{ID: "id2", Plugin: "testmodule", UpdatePriority: 2, RebootPriority: 2},
			{ID: "id3", Plugin: "testmodule", UpdatePriority: 1, RebootPriority: 3}}}

	components = make([]*testModule, 0)
	storage := newTestStorage()

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

	infos, err := createUpdateInfos(currentStatus.Components)
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

	testOperation(t, handler, func() { handler.PrepareUpdate(infos) }, &newStatus, nil,
		[]orderInfo{{"id1", opPrepare}, {"id2", opPrepare}, {"id3", opPrepare}})

	// Update

	for _, component := range components {
		component.rebootRequired = true
	}

	newStatus.State = umclient.StateUpdated

	testOperation(t, handler, handler.StartUpdate, &newStatus, nil,
		[]orderInfo{
			{"id1", opUpdate}, {"id2", opUpdate}, {"id3", opUpdate},
			{"id3", opReboot}, {"id2", opReboot}, {"id1", opReboot},
			{"id1", opUpdate}, {"id2", opUpdate}, {"id3", opUpdate}})

	// Apply

	for _, component := range components {
		component.rebootRequired = true
	}

	finalStatus := umclient.Status{State: umclient.StateIdle}

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
			{"id1", opApply}, {"id2", opApply}, {"id3", opApply},
			{"id3", opReboot}, {"id2", opReboot}, {"id1", opReboot},
			{"id1", opApply}, {"id2", opApply}, {"id3", opApply}})
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func newTestStorage() (storage *testStorage) {
	return &testStorage{aosVersions: make(map[string]uint64)}
}

func (storage *testStorage) SetUpdateState(state []byte) (err error) {
	storage.updateState = state

	return nil
}

func (storage *testStorage) GetUpdateState() (state []byte, err error) {
	return storage.updateState, nil
}

func (storage *testStorage) SetAosVersion(id string, version uint64) (err error) {
	storage.aosVersions[id] = version

	return nil
}

func (storage *testStorage) GetAosVersion(id string) (version uint64, err error) {
	return storage.aosVersions[id], nil
}

func (storage *testStorage) GetModuleState(id string) (state []byte, err error) {
	return []byte("valid"), nil
}

func (storage *testStorage) SetModuleState(id string, state []byte) (err error) {
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

	return err
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

	return err
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

	return err
}

func (module *testModule) Close() (err error) {
	err = module.status
	module.status = nil

	return err
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
			return fmt.Errorf("wrong component %s ops: %v", id, orderOps)
		}
	}

	return nil
}

func waitForStatus(handler *updatehandler.Handler, expectedStatus *umclient.Status) (err error) {
	select {
	case <-time.After(5 * time.Second):
		return errors.New("wait operation timeout")

	case currentStatus := <-handler.StatusChannel():
		if expectedStatus == nil {
			return nil
		}

		if currentStatus.State != expectedStatus.State {
			return fmt.Errorf("wrong current state: %s", currentStatus.State)
		}

		if currentStatus.Error != expectedStatus.Error {
			return fmt.Errorf("wrong error value: %s", currentStatus.Error)
		}

		for _, expectedItem := range expectedStatus.Components {
			index := len(expectedStatus.Components)

			for i, currentItem := range currentStatus.Components {
				if reflect.DeepEqual(currentItem, expectedItem) {
					index = i

					break
				}
			}

			if index == len(expectedStatus.Components) {
				return fmt.Errorf("expected item not found: %v", expectedItem)
			}

			currentStatus.Components = append(currentStatus.Components[:index], currentStatus.Components[index+1:]...)
		}

		if len(currentStatus.Components) != 0 {
			return fmt.Errorf("unexpected item found: %v", currentStatus.Components[0])
		}

		return nil
	}
}

func createImage(imagePath string) (fileInfo image.FileInfo, err error) {
	if err := exec.Command("dd", "if=/dev/null", "of="+imagePath, "bs=1M", "count=8").Run(); err != nil {
		return fileInfo, err
	}

	return image.CreateFileInfo(imagePath)
}

func createUpdateInfos(currentStatus []umclient.ComponentStatusInfo) (infos []umclient.ComponentUpdateInfo, err error) {
	for _, status := range currentStatus {
		imagePath := path.Join(tmpDir, fmt.Sprintf("testimage_%s.bin", status.ID))

		imageInfo, err := createImage(imagePath)
		if err != nil {
			return nil, err
		}

		info := umclient.ComponentUpdateInfo{
			ID:         status.ID,
			AosVersion: status.AosVersion + 1,
			URL:        "file://" + imagePath,
			Sha256:     imageInfo.Sha256,
			Sha512:     imageInfo.Sha512,
			Size:       imageInfo.Size,
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
	expectedOrder []orderInfo) {
	t.Helper()

	order = nil

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
