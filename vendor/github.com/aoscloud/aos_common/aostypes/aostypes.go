// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2022 Renesas Electronics Corporation.
// Copyright (C) 2022 EPAM Systems, Inc.
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

package aostypes

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	imagespec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/aoscloud/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

//nolint:revive
const (
	alternativePattern = `^P(((?P<year>\d+)-)((?P<month>\d+)-)((?P<day>\d+)))?(T((?P<hour>\d+):)((?P<minute>\d+):)(?P<second>\d+))?$`
	canonicPattern     = `^P((?P<year>\d+)Y)?((?P<month>\d+)M)?((?P<week>\d+)W)?((?P<day>\d+)D)?(T((?P<hour>\d+)H)?((?P<minute>\d+)M)?((?P<second>\d+)S)?)?$`
)

const (
	dayDuration   = 24 * time.Hour
	weekDuration  = 7 * dayDuration
	yearDuration  = 365*dayDuration + 6*time.Hour
	monthDuration = yearDuration / 12
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Duration represents duration in format "00:00:00".
type Duration struct {
	time.Duration
}

// Time represents time in format "00:00:00".
type Time struct {
	time.Time
}

// AlertRuleParam describes alert rule.
type AlertRuleParam struct {
	MinTimeout   Duration `json:"minTimeout"`
	MinThreshold uint64   `json:"minThreshold"`
	MaxThreshold uint64   `json:"maxThreshold"`
}

// PartitionAlertRuleParam describes alert rule.
type PartitionAlertRuleParam struct {
	AlertRuleParam
	Name string `json:"name"`
}

// AlertRules define service monitoring alerts rules.
type AlertRules struct {
	RAM        *AlertRuleParam           `json:"ram,omitempty"`
	CPU        *AlertRuleParam           `json:"cpu,omitempty"`
	UsedDisks  []PartitionAlertRuleParam `json:"usedDisks,omitempty"`
	InTraffic  *AlertRuleParam           `json:"inTraffic,omitempty"`
	OutTraffic *AlertRuleParam           `json:"outTraffic,omitempty"`
}

// FileSystemMount specifies a mount instructions.
type FileSystemMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Host struct represents entry in /etc/hosts.
type Host struct {
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
	Hosts  []Host            `json:"hosts,omitempty"`
}

// NodeConfig node configuration.
type NodeUnitConfig struct {
	NodeType  string         `json:"nodeType"`
	Devices   []DeviceInfo   `json:"devices,omitempty"`
	Resources []ResourceInfo `json:"resources,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Priority  uint32         `json:"priority,omitempty"`
}

// UnitConfig board configuration.
type UnitConfig struct {
	FormatVersion uint64           `json:"formatVersion"`
	VendorVersion string           `json:"vendorVersion"`
	Nodes         []NodeUnitConfig `json:"nodes"`
}

// ServiceInfo service info.
type ServiceInfo struct {
	VersionInfo
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	GID        uint32 `json:"gid"`
	URL        string `json:"url"`
	Sha256     []byte `json:"sha256"`
	Sha512     []byte `json:"sha512"`
	Size       uint64 `json:"size"`
}

// LayerInfo layer info.
type LayerInfo struct {
	VersionInfo
	ID     string `json:"id"`
	Digest string `json:"digest"`
	URL    string `json:"url"`
	Sha256 []byte `json:"sha256"`
	Sha512 []byte `json:"sha512"`
	Size   uint64 `json:"size"`
}

// VersionInfo common version structure.
type VersionInfo struct {
	AosVersion    uint64 `json:"aosVersion"`
	VendorVersion string `json:"vendorVersion"`
	Description   string `json:"description"`
}

// InstanceIdent instance identification information.
type InstanceIdent struct {
	ServiceID string `json:"serviceId"`
	SubjectID string `json:"subjectId"`
	Instance  uint64 `json:"instance"`
}

// FirewallRule firewall rule.
type FirewallRule struct {
	DstIP   string `json:"dstIp"`
	DstPort string `json:"dstPort"`
	Proto   string `json:"proto"`
	SrcIP   string `json:"srcIp"`
}

// NetworkParameters networks parameters.
type NetworkParameters struct {
	NetworkID     string
	Subnet        string
	IP            string
	VlanID        uint64
	DNSServers    []string
	FirewallRules []FirewallRule
}

// InstanceInfo instance information to start it.
type InstanceInfo struct {
	InstanceIdent
	NetworkParameters
	UID         uint32 `json:"uid"`
	Priority    uint64 `json:"priority"`
	StoragePath string `json:"storagePath"`
	StatePath   string `json:"statePath"`
}

// ServiceManifest Aos service manifest.
type ServiceManifest struct {
	imagespec.Manifest
	AosService *imagespec.Descriptor `json:"aosService,omitempty"`
}

// ServiceDevice struct with service divices rules.
type ServiceDevice struct {
	Name        string `json:"name"`
	Permissions string `json:"permissions"`
}

// ServiceQuotas service quotas representation.
type ServiceQuotas struct {
	CPULimit      *uint64 `json:"cpuLimit,omitempty"`
	RAMLimit      *uint64 `json:"ramLimit,omitempty"`
	PIDsLimit     *uint64 `json:"pidsLimit,omitempty"`
	NoFileLimit   *uint64 `json:"noFileLimit,omitempty"`
	TmpLimit      *uint64 `json:"tmpLimit,omitempty"`
	StateLimit    *uint64 `json:"stateLimit,omitempty"`
	StorageLimit  *uint64 `json:"storageLimit,omitempty"`
	UploadSpeed   *uint64 `json:"uploadSpeed,omitempty"`
	DownloadSpeed *uint64 `json:"downloadSpeed,omitempty"`
	UploadLimit   *uint64 `json:"uploadLimit,omitempty"`
	DownloadLimit *uint64 `json:"downloadLimit,omitempty"`
}

// RunParameters service startup parameters.
type RunParameters struct {
	StartInterval   Duration `json:"startInterval,omitempty"`
	StartBurst      uint     `json:"startBurst,omitempty"`
	RestartInterval Duration `json:"restartInterval,omitempty"`
}

// ServiceConfig Aos service configuration.
type ServiceConfig struct {
	Created            time.Time                    `json:"created"`
	Author             string                       `json:"author"`
	Hostname           *string                      `json:"hostname,omitempty"`
	Runner             string                       `json:"runner"`
	Sysctl             map[string]string            `json:"sysctl,omitempty"`
	OfflineTTL         Duration                     `json:"offlineTtl,omitempty"`
	Quotas             ServiceQuotas                `json:"quotas"`
	AllowedConnections map[string]struct{}          `json:"allowedConnections,omitempty"`
	Devices            []ServiceDevice              `json:"devices,omitempty"`
	Resources          []string                     `json:"resources,omitempty"`
	Permissions        map[string]map[string]string `json:"permissions,omitempty"`
	AlertRules         *AlertRules                  `json:"alertRules,omitempty"`
	RunParameters      RunParameters                `json:"runParameters,omitempty"`
}

/***********************************************************************************************************************
 * Interfaces
 **********************************************************************************************************************/

// MarshalJSON marshals JSON Time type.
func (t Time) MarshalJSON() (b []byte, err error) {
	if b, err = json.Marshal(t.Format("15:04:05")); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return b, nil
}

// UnmarshalJSON unmarshals JSON Time type.
func (t *Time) UnmarshalJSON(b []byte) (err error) {
	const errFormat = "invalid time value: %v"

	var v interface{}

	if err := json.Unmarshal(b, &v); err != nil {
		return aoserrors.Wrap(err)
	}

	switch value := v.(type) {
	// Convert ISO 8601 to time.Time
	case string:
		var strFields []string

		if strings.Contains(value, ":") {
			strFields = strings.Split(strings.TrimLeft(value, "T"), ":")
		} else {
			if !strings.HasPrefix(value, "T") {
				return aoserrors.Errorf(errFormat, value)
			}

			for i := 1; i < len(value); i += 2 {
				strFields = append(strFields, value[i:i+2])
			}
		}

		if len(strFields) == 0 {
			return aoserrors.Errorf(errFormat, value)
		}

		intFields := make([]int, 3) //nolint:gomnd //time format has 3 fields HH:MM:SS

		for i, field := range strFields {
			if intFields[i], err = strconv.Atoi(field); err != nil {
				return aoserrors.Errorf(errFormat, value)
			}
		}

		t.Time = time.Date(0, 1, 1, intFields[0], intFields[1], intFields[2], 0, time.Local) //nolint:gosmopolitan

		return nil

	default:
		return aoserrors.Errorf(errFormat, value)
	}
}

// MarshalJSON marshals JSON Duration type.
func (d Duration) MarshalJSON() (b []byte, err error) {
	if b, err = json.Marshal(d.Duration.String()); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return b, nil
}

// UnmarshalJSON unmarshals JSON Duration type.
func (d *Duration) UnmarshalJSON(b []byte) (err error) {
	var v interface{}

	if err := json.Unmarshal(b, &v); err != nil {
		return aoserrors.Wrap(err)
	}

	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)

		return nil

	case string:
		if !strings.HasPrefix(value, "P") {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return aoserrors.Wrap(err)
			}

			d.Duration = duration
		} else {
			duration, err := parseISO8601Duration(value)
			if err != nil {
				return aoserrors.Wrap(err)
			}

			d.Duration = duration
		}

		return nil

	default:
		return aoserrors.Errorf("invalid duration value: %v", value)
	}
}

func parseISO8601Duration(value string) (time.Duration, error) {
	var (
		patternStr = canonicPattern
		match      []string
		d          time.Duration
	)

	if strings.Contains(value, "-") || strings.Contains(value, ":") {
		patternStr = alternativePattern
	}

	pattern := regexp.MustCompile(patternStr)

	if !pattern.MatchString(value) {
		return d, aoserrors.New("could not parse duration string")
	}

	match = pattern.FindStringSubmatch(value)

	for i, name := range pattern.SubexpNames() {
		part := match[i]
		if i == 0 || name == "" || part == "" {
			continue
		}

		val, err := strconv.Atoi(part)
		if err != nil {
			return d, aoserrors.Wrap(err)
		}

		switch name {
		case "year":
			d += time.Duration(val) * yearDuration
		case "month":
			d += time.Duration(val) * monthDuration
		case "week":
			d += time.Duration(val) * weekDuration
		case "day":
			d += time.Duration(val) * dayDuration
		case "hour":
			d += time.Duration(val) * time.Hour
		case "minute":
			d += time.Duration(val) * time.Minute
		case "second":
			d += time.Duration(val) * time.Second
		default:
			return d, aoserrors.Errorf("unknown field %s", name)
		}
	}

	return d, nil
}
