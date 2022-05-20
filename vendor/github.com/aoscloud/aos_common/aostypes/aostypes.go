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
	"strconv"
	"strings"
	"time"

	"github.com/aoscloud/aos_common/aoserrors"
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

// AlertRule describes alert rule.
type AlertRule struct {
	MinTimeout   Duration `json:"minTimeout"`
	MinThreshold uint64   `json:"minThreshold"`
	MaxThreshold uint64   `json:"maxThreshold"`
}

// ServiceAlertRules define service monitoring alerts rules.
type ServiceAlertRules struct {
	RAM        *AlertRule `json:"ram,omitempty"`
	CPU        *AlertRule `json:"cpu,omitempty"`
	UsedDisk   *AlertRule `json:"usedDisk,omitempty"`
	InTraffic  *AlertRule `json:"inTraffic,omitempty"`
	OutTraffic *AlertRule `json:"outTraffic,omitempty"`
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

// BoardConfig board configuration.
type BoardConfig struct {
	FormatVersion uint64         `json:"formatVersion"`
	VendorVersion string         `json:"vendorVersion"`
	Devices       []DeviceInfo   `json:"devices"`
	Resources     []ResourceInfo `json:"resources"`
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

		intFields := make([]int, 3) // nolint:gomnd //time format has 3 fields HH:MM:SS

		for i, field := range strFields {
			if intFields[i], err = strconv.Atoi(field); err != nil {
				return aoserrors.Errorf(errFormat, value)
			}
		}

		t.Time = time.Date(0, 1, 1, intFields[0], intFields[1], intFields[2], 0, time.Local)

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
		duration, err := time.ParseDuration(value)
		if err != nil {
			return aoserrors.Wrap(err)
		}

		d.Duration = duration

		return nil

	default:
		return aoserrors.Errorf("invalid duration value: %v", value)
	}
}
