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
	"fmt"

	"github.com/aosedge/aos_common/aostypes"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// DesiredStatusMessageType desired status message type.
const DesiredStatusMessageType = "desiredStatus"

// SOTA/FOTA schedule type.
const (
	ForceUpdate     = "force"
	TriggerUpdate   = "trigger"
	TimetableUpdate = "timetable"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// FileSystemMount specifies a mount instructions.
type FileSystemMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// HostInfo struct represents entry in /etc/hosts.
type HostInfo struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

// DeviceInfo device information.
type DeviceInfo struct {
	Name        string   `json:"name"`
	SharedCount int      `json:"sharedCount,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	HostDevices []string `json:"hostDevices"`
}

// ResourceInfo resource information.
type ResourceInfo struct {
	Name   string            `json:"name"`
	Groups []string          `json:"groups,omitempty"`
	Mounts []FileSystemMount `json:"mounts,omitempty"`
	Env    []string          `json:"env,omitempty"`
	Hosts  []HostInfo        `json:"hosts,omitempty"`
}

// NodeConfig node configuration.
type NodeConfig struct {
	NodeID         *string                      `json:"nodeId,omitempty"`
	NodeType       string                       `json:"nodeType"`
	ResourceRatios *aostypes.ResourceRatiosInfo `json:"resourceRatios,omitempty"`
	AlertRules     *aostypes.AlertRules         `json:"alertRules,omitempty"`
	Devices        []DeviceInfo                 `json:"devices,omitempty"`
	Resources      []ResourceInfo               `json:"resources,omitempty"`
	Labels         []string                     `json:"labels,omitempty"`
	Priority       uint32                       `json:"priority,omitempty"`
}

// UnitConfig unit configuration.
type UnitConfig struct {
	FormatVersion interface{}  `json:"formatVersion"`
	Version       string       `json:"version"`
	Nodes         []NodeConfig `json:"nodes"`
}

// Signs message signature.
type Signs struct {
	ChainName        string   `json:"chainName"`
	Alg              string   `json:"alg"`
	Value            []byte   `json:"value"`
	TrustedTimestamp string   `json:"trustedTimestamp"`
	OcspValues       []string `json:"ocspValues"`
}

// DecryptionInfo update decryption info.
type DecryptionInfo struct {
	BlockAlg     string `json:"blockAlg"`
	BlockIv      []byte `json:"blockIv"`
	BlockKey     []byte `json:"blockKey"`
	AsymAlg      string `json:"asymAlg"`
	ReceiverInfo *struct {
		Serial string `json:"serial"`
		Issuer []byte `json:"issuer"`
	} `json:"receiverInfo"`
}

// DownloadInfo struct contains how to download item.
type DownloadInfo struct {
	URLs   []string `json:"urls"`
	Sha256 []byte   `json:"sha256"`
	Size   uint64   `json:"size"`
}

// NodeStatus node status.
type NodeStatus struct {
	NodeID string `json:"nodeId"`
	Status string `json:"status"`
}

// ServiceInfo decrypted service info.
type ServiceInfo struct {
	ServiceID  string `json:"id"`
	ProviderID string `json:"providerId"`
	Version    string `json:"version"`
	DownloadInfo
	DecryptionInfo DecryptionInfo `json:"decryptionInfo"`
	Signs          Signs          `json:"signs"`
}

// LayerInfo decrypted layer info.
type LayerInfo struct {
	LayerID string `json:"id"`
	Digest  string `json:"digest"`
	Version string `json:"version"`
	DownloadInfo
	DecryptionInfo DecryptionInfo `json:"decryptionInfo"`
	Signs          Signs          `json:"signs"`
}

// ComponentInfo decrypted component info.
type ComponentInfo struct {
	ComponentID   *string         `json:"id,omitempty"`
	ComponentType string          `json:"type"`
	Version       string          `json:"version"`
	Annotations   json.RawMessage `json:"annotations,omitempty"`
	DownloadInfo
	DecryptionInfo DecryptionInfo `json:"decryptionInfo"`
	Signs          Signs          `json:"signs"`
}

// InstanceInfo decrypted desired instance runtime info.
type InstanceInfo struct {
	ServiceID    string   `json:"serviceId"`
	SubjectID    string   `json:"subjectId"`
	Priority     uint64   `json:"priority"`
	NumInstances uint64   `json:"numInstances"`
	Labels       []string `json:"labels"`
}

// Certificate certificate structure.
type Certificate struct {
	Certificate []byte `json:"certificate"`
	Fingerprint string `json:"fingerprint"`
}

// CertificateChain  certificate chain.
type CertificateChain struct {
	Name         string   `json:"name"`
	Fingerprints []string `json:"fingerprints"`
}

// TimeSlot time slot with start and finish time.
type TimeSlot struct {
	Start aostypes.Time `json:"start"`
	End   aostypes.Time `json:"end"`
}

// TimetableEntry entry for update timetable.
type TimetableEntry struct {
	DayOfWeek uint       `json:"dayOfWeek"`
	TimeSlots []TimeSlot `json:"timeSlots"`
}

// ScheduleRule rule for performing schedule update.
type ScheduleRule struct {
	TTL       uint64           `json:"ttl"`
	Type      string           `json:"type"`
	Timetable []TimetableEntry `json:"timetable"`
}

// DesiredStatus desired status.
type DesiredStatus struct {
	MessageType       string             `json:"messageType"`
	UnitConfig        *UnitConfig        `json:"unitConfig,omitempty"`
	Nodes             []NodeStatus       `json:"nodes"`
	Components        []ComponentInfo    `json:"components"`
	Layers            []LayerInfo        `json:"layers"`
	Services          []ServiceInfo      `json:"services"`
	Instances         []InstanceInfo     `json:"instances"`
	FOTASchedule      ScheduleRule       `json:"fotaSchedule"`
	SOTASchedule      ScheduleRule       `json:"sotaSchedule"`
	Certificates      []Certificate      `json:"certificates,omitempty"`
	CertificateChains []CertificateChain `json:"certificateChains,omitempty"`
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

func (service ServiceInfo) String() string {
	return fmt.Sprintf("{id: %s, version: %s}", service.ServiceID, service.Version)
}

func (layer LayerInfo) String() string {
	return fmt.Sprintf("{id: %s, digest: %s, version: %s}", layer.LayerID, layer.Digest, layer.Version)
}

func (component ComponentInfo) String() string {
	id := "none"

	if component.ComponentID != nil {
		id = *component.ComponentID
	}

	return fmt.Sprintf("{id: %s, type: %s, annotations: %s, version: %s}",
		id, component.ComponentType, component.Annotations, component.Version)
}
