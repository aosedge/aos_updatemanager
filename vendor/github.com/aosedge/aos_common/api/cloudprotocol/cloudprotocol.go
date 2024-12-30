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

import "encoding/json"

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// ProtocolVersion specifies supported protocol version.
const ProtocolVersion = 6

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// ReceivedMessage structure for Aos incoming messages.
type ReceivedMessage struct {
	Header MessageHeader   `json:"header"`
	Data   json.RawMessage `json:"data"`
}

// Message structure for AOS messages.
type Message struct {
	Header MessageHeader `json:"header"`
	Data   interface{}   `json:"data"`
}

// MessageHeader message header.
type MessageHeader struct {
	Version  uint64 `json:"version"`
	SystemID string `json:"systemId"`
}

// ErrorInfo error information.
type ErrorInfo struct {
	AosCode  int    `json:"aosCode"`
	ExitCode int    `json:"exitCode"`
	Message  string `json:"message,omitempty"`
}

// InstanceFilter instance filter structure.
type InstanceFilter struct {
	ServiceID *string `json:"serviceId,omitempty"`
	SubjectID *string `json:"subjectId,omitempty"`
	Instance  *uint64 `json:"instance,omitempty"`
}
