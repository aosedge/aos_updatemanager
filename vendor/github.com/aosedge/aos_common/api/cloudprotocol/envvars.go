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

import "time"

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// EnvVars message types.
const (
	OverrideEnvVarsMessageType       = "overrideEnvVars"
	OverrideEnvVarsStatusMessageType = "overrideEnvVarsStatus"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// EnvVarsInstanceInfo struct with envs and related service and user.
type EnvVarsInstanceInfo struct {
	InstanceFilter
	Variables []EnvVarInfo `json:"variables"`
}

// EnvVarInfo env info with id and time to live.
type EnvVarInfo struct {
	Name  string     `json:"name"`
	Value string     `json:"value"`
	TTL   *time.Time `json:"ttl"`
}

// EnvVarsInstanceStatus struct with envs status and related service and user.
type EnvVarsInstanceStatus struct {
	InstanceFilter
	Statuses []EnvVarStatus `json:"statuses"`
}

// EnvVarStatus env status with error message.
type EnvVarStatus struct {
	Name      string     `json:"name"`
	ErrorInfo *ErrorInfo `json:"error,omitempty"`
}

// OverrideEnvVars request to override service environment variables.
type OverrideEnvVars struct {
	MessageType string                `json:"messageType"`
	Items       []EnvVarsInstanceInfo `json:"items"`
}

// OverrideEnvVarsStatus override env status.
type OverrideEnvVarsStatus struct {
	MessageType string                  `json:"messageType"`
	Statuses    []EnvVarsInstanceStatus `json:"statuses"`
}
