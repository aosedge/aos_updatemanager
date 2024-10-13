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

import "github.com/aosedge/aos_common/aostypes"

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// State message types.
const (
	StateAcceptanceMessageType = "stateAcceptance"
	UpdateStateMessageType     = "updateState"
	NewStateMessageType        = "newState"
	StateRequestMessageType    = "stateRequest"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// StateAcceptance state acceptance message.
type StateAcceptance struct {
	MessageType string `json:"messageType"`
	aostypes.InstanceIdent
	Checksum string `json:"checksum"`
	Result   string `json:"result"`
	Reason   string `json:"reason"`
}

// UpdateState state update message.
type UpdateState struct {
	MessageType string `json:"messageType"`
	aostypes.InstanceIdent
	Checksum string `json:"stateChecksum"`
	State    string `json:"state"`
}

// NewState new state structure.
type NewState struct {
	MessageType string `json:"messageType"`
	aostypes.InstanceIdent
	Checksum string `json:"stateChecksum"`
	State    string `json:"state"`
}

// StateRequest state request structure.
type StateRequest struct {
	MessageType string `json:"messageType"`
	aostypes.InstanceIdent
	Default bool `json:"default"`
}
