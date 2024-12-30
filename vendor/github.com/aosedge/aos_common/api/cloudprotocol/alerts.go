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

package cloudprotocol

import (
	"time"

	"github.com/aosedge/aos_common/aostypes"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// AlertsMessageType alerts message type.
const AlertsMessageType = "alerts"

// Alert tags.
const (
	AlertTagSystemError      = "systemAlert"
	AlertTagAosCore          = "coreAlert"
	AlertTagResourceValidate = "resourceValidateAlert"
	AlertTagDeviceAllocate   = "deviceAllocateAlert"
	AlertTagSystemQuota      = "systemQuotaAlert"
	AlertTagInstanceQuota    = "instanceQuotaAlert"
	AlertTagDownloadProgress = "downloadProgressAlert"
	AlertTagServiceInstance  = "serviceInstanceAlert"
)

// Download target types.
const (
	DownloadTargetComponent = "component"
	DownloadTargetLayer     = "layer"
	DownloadTargetService   = "service"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// AlertItem common alert data.
type AlertItem struct {
	Timestamp time.Time `json:"timestamp"`
	Tag       string    `json:"tag"`
}

// SystemAlert system alert structure.
type SystemAlert struct {
	AlertItem
	NodeID  string `json:"nodeId"`
	Message string `json:"message"`
}

// CoreAlert system alert structure.
type CoreAlert struct {
	AlertItem
	NodeID        string `json:"nodeId"`
	CoreComponent string `json:"coreComponent"`
	Message       string `json:"message"`
}

// DownloadAlert download alert structure.
type DownloadAlert struct {
	AlertItem
	TargetType      string `json:"targetType"`
	TargetID        string `json:"targetId"`
	Version         string `json:"version"`
	Message         string `json:"message"`
	URL             string `json:"url"`
	DownloadedBytes string `json:"downloadedBytes"`
	TotalBytes      string `json:"totalBytes"`
}

// SystemQuotaAlert system quota alert structure.
type SystemQuotaAlert struct {
	AlertItem
	NodeID    string `json:"nodeId"`
	Parameter string `json:"parameter"`
	Value     uint64 `json:"value"`
	Status    string `json:"-"`
}

// InstanceQuotaAlert instance quota alert structure.
type InstanceQuotaAlert struct {
	AlertItem
	aostypes.InstanceIdent
	Parameter string `json:"parameter"`
	Value     uint64 `json:"value"`
	Status    string `json:"-"`
}

// DeviceAllocateAlert device allocate alert structure.
type DeviceAllocateAlert struct {
	AlertItem
	aostypes.InstanceIdent
	NodeID  string `json:"nodeId"`
	Device  string `json:"device"`
	Message string `json:"message"`
}

// ResourceValidateAlert resource validate alert structure.
type ResourceValidateAlert struct {
	AlertItem
	NodeID string      `json:"nodeId"`
	Name   string      `json:"name"`
	Errors []ErrorInfo `json:"errors"`
}

// ServiceInstanceAlert system alert structure.
type ServiceInstanceAlert struct {
	AlertItem
	aostypes.InstanceIdent
	ServiceVersion string `json:"version"`
	Message        string `json:"message"`
}

// Alerts alerts message structure.
type Alerts struct {
	MessageType string        `json:"messageType"`
	Items       []interface{} `json:"items"`
}
