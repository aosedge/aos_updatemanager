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
	"encoding/json"
	"strings"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/aostypes"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// UnitStatusMessageType unit status message type.
const UnitStatusMessageType = "unitStatus"

// Instance statuses.
const (
	InstanceStateActivating = "activating"
	InstanceStateActive     = "active"
	InstanceStateInactive   = "inactive"
	InstanceStateFailed     = "failed"
)

// Service/layers/components statuses.
const (
	UnknownStatus     = "unknown"
	PendingStatus     = "pending"
	DownloadingStatus = "downloading"
	DownloadedStatus  = "downloaded"
	InstallingStatus  = "installing"
	InstalledStatus   = "installed"
	RemovingStatus    = "removing"
	RemovedStatus     = "removed"
	ErrorStatus       = "error"
)

// Partition types.
const (
	GenericPartition  = "generic"
	StoragesPartition = "storages"
	StatesPartition   = "states"
	ServicesPartition = "services"
	LayersPartition   = "layers"
)

// Node statuses.
const (
	NodeStatusUnprovisioned = "unprovisioned"
	NodeStatusProvisioned   = "provisioned"
	NodeStatusPaused        = "paused"
	NodeStatusError         = "error"
)

// Node attribute names.
const (
	NodeAttrMainNode      = "MainNode"
	NodeAttrAosComponents = "AosComponents"
	NodeAttrRunners       = "NodeRunners"
)

// Node attribute values.
const (
	AosComponentCM  = "cm"
	AosComponentIAM = "iam"
	AosComponentSM  = "sm"
	AosComponentUM  = "um"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// UnitConfigStatus unit config status.
type UnitConfigStatus struct {
	Version   string     `json:"version"`
	Status    string     `json:"status"`
	ErrorInfo *ErrorInfo `json:"errorInfo,omitempty"`
}

// CPUInfo cpu information.
type CPUInfo struct {
	ModelName  string `json:"modelName"`
	NumCores   uint64 `json:"totalNumCores"`
	NumThreads uint64 `json:"totalNumThreads"`
	Arch       string `json:"arch"`
	ArchFamily string `json:"archFamily"`
	MaxDMIPs   uint64 `json:"maxDmips"`
}

// PartitionInfo partition information.
type PartitionInfo struct {
	Name      string   `json:"name"`
	Types     []string `json:"types"`
	TotalSize uint64   `json:"totalSize"`
	Path      string   `json:"-"`
}

// NodeInfo node information.
type NodeInfo struct {
	NodeID     string                 `json:"id"`
	NodeType   string                 `json:"type"`
	Name       string                 `json:"name"`
	Status     string                 `json:"status"`
	CPUs       []CPUInfo              `json:"cpus,omitempty"`
	OSType     string                 `json:"osType"`
	MaxDMIPs   uint64                 `json:"maxDmips"`
	TotalRAM   uint64                 `json:"totalRam"`
	Attrs      map[string]interface{} `json:"attrs,omitempty"`
	Partitions []PartitionInfo        `json:"partitions,omitempty"`
	ErrorInfo  *ErrorInfo             `json:"errorInfo,omitempty"`
}

// ServiceStatus service status.
type ServiceStatus struct {
	ServiceID string     `json:"id"`
	Version   string     `json:"version"`
	Status    string     `json:"status"`
	ErrorInfo *ErrorInfo `json:"errorInfo,omitempty"`
}

// InstanceStatus service instance runtime status.
type InstanceStatus struct {
	aostypes.InstanceIdent
	ServiceVersion string     `json:"version"`
	StateChecksum  string     `json:"stateChecksum,omitempty"`
	Status         string     `json:"status"`
	NodeID         string     `json:"nodeId"`
	ErrorInfo      *ErrorInfo `json:"errorInfo,omitempty"`
}

// LayerStatus layer status.
type LayerStatus struct {
	LayerID   string     `json:"id"`
	Digest    string     `json:"digest"`
	Version   string     `json:"version"`
	Status    string     `json:"status"`
	ErrorInfo *ErrorInfo `json:"errorInfo,omitempty"`
}

// ComponentStatus component status.
type ComponentStatus struct {
	ComponentID   string          `json:"id"`
	ComponentType string          `json:"type"`
	Version       string          `json:"version"`
	NodeID        *string         `json:"nodeId,omitempty"`
	Status        string          `json:"status"`
	Annotations   json.RawMessage `json:"annotations,omitempty"`
	ErrorInfo     *ErrorInfo      `json:"errorInfo,omitempty"`
}

// UnitStatus unit status structure.
type UnitStatus struct {
	MessageType  string             `json:"messageType"`
	IsDeltaInfo  bool               `json:"isDeltaInfo"`
	UnitConfig   []UnitConfigStatus `json:"unitConfig"`
	Nodes        []NodeInfo         `json:"nodes"`
	Services     []ServiceStatus    `json:"services"`
	Instances    []InstanceStatus   `json:"instances"`
	Layers       []LayerStatus      `json:"layers,omitempty"`
	Components   []ComponentStatus  `json:"components"`
	UnitSubjects []string           `json:"unitSubjects"`
}

// DeltaUnitStatus delta unit status structure.
type DeltaUnitStatus struct {
	MessageType  string             `json:"messageType"`
	IsDeltaInfo  bool               `json:"isDeltaInfo"`
	UnitConfig   []UnitConfigStatus `json:"unitConfig,omitempty"`
	Nodes        []NodeInfo         `json:"nodes,omitempty"`
	Services     []ServiceStatus    `json:"services,omitempty"`
	Instances    []InstanceStatus   `json:"instances,omitempty"`
	Layers       []LayerStatus      `json:"layers,omitempty"`
	Components   []ComponentStatus  `json:"components,omitempty"`
	UnitSubjects []string           `json:"unitSubjects,omitempty"`
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

func (nodeInfo *NodeInfo) IsMainNode() bool {
	_, ok := nodeInfo.Attrs[NodeAttrMainNode]

	return ok
}

func (nodeInfo *NodeInfo) GetAosComponents() ([]string, error) {
	return nodeInfo.attrToStringSlice(NodeAttrAosComponents)
}

func (nodeInfo *NodeInfo) GetNodeRunners() ([]string, error) {
	return nodeInfo.attrToStringSlice(NodeAttrRunners)
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func (nodeInfo *NodeInfo) attrToStringSlice(attrName string) ([]string, error) {
	attrValue, ok := nodeInfo.Attrs[attrName]
	if !ok {
		return nil, aoserrors.Errorf("attribute %s not found", attrName)
	}

	attrString, ok := attrValue.(string)
	if !ok {
		return nil, aoserrors.New("invalid attribute type")
	}

	values := strings.Split(attrString, ",")

	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}

	return values, nil
}
