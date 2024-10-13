// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
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

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

// Provisioning message types.
const (
	StartProvisioningRequestMessageType   = "startProvisioningRequest"
	StartProvisioningResponseMessageType  = "startProvisioningResponse"
	FinishProvisioningRequestMessageType  = "finishProvisioningRequest"
	FinishProvisioningResponseMessageType = "finishProvisioningResponse"
	DeprovisioningRequestMessageType      = "deprovisioningRequest"
	DeprovisioningResponseMessageType     = "deprovisioningResponse"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// StartProvisioningRequest message.
type StartProvisioningRequest struct {
	MessageType string `json:"messageType"`
	NodeID      string `json:"nodeId"`
	Password    string `json:"password"`
}

// StartProvisioningResponse message.
type StartProvisioningResponse struct {
	MessageType string          `json:"messageType"`
	NodeID      string          `json:"nodeId"`
	ErrorInfo   *ErrorInfo      `json:"errorInfo,omitempty"`
	CSRs        []IssueCertData `json:"csrs"`
}

// FinishProvisioningRequest message.
type FinishProvisioningRequest struct {
	MessageType  string           `json:"messageType"`
	NodeID       string           `json:"nodeId"`
	Certificates []IssuedCertData `json:"certificates"`
	Password     string           `json:"password"`
}

// FinishProvisioningResponse message.
type FinishProvisioningResponse struct {
	MessageType string     `json:"messageType"`
	NodeID      string     `json:"nodeId"`
	ErrorInfo   *ErrorInfo `json:"errorInfo,omitempty"`
}

// DeprovisioningRequest message.
type DeprovisioningRequest struct {
	MessageType string `json:"messageType"`
	NodeID      string `json:"nodeId"`
	Password    string `json:"password"`
}

// DeprovisioningResponse message.
type DeprovisioningResponse struct {
	MessageType string     `json:"messageType"`
	NodeID      string     `json:"nodeId"`
	ErrorInfo   *ErrorInfo `json:"errorInfo,omitempty"`
}
