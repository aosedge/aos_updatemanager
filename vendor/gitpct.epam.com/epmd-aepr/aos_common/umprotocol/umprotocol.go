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

package umprotocol

import (
	"encoding/json"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

// Message types
const (
	UpgradeRequestType     = "upgradeRequest"
	RevertRequestType      = "revertRequest"
	StatusRequestType      = "statusRequest"
	StatusResponseType     = "statusResponse"
	CreateKeysRequestType  = "createKeysRequest"
	CreateKeysResponseType = "createKeysResponse"
	ApplyCertRequestType   = "applyCertRequest"
	ApplyCertResponseType  = "applyCertResponse"
	GetCertRequestType     = "getCertRequest"
	GetCertResponseType    = "getCertResponse"
)

// Operation status
const (
	SuccessStatus    = "success"
	FailedStatus     = "failed"
	InProgressStatus = "inProgress"
)

// Operation type
const (
	UpgradeOperation = "upgrade"
	RevertOperation  = "revert"
)

// Version specifies the protocol version
const Version = 1

/*******************************************************************************
 * Types
 ******************************************************************************/

// Header UM message header
type Header struct {
	Version     uint64 `json:"version"`
	MessageType string `json:"messageType"`
}

// Message UM message structure
type Message struct {
	Header Header          `json:"header"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// ImageInfo upgrade image info
type ImageInfo struct {
	Path   string `json:"path"`
	Sha256 []byte `json:"sha256"`
	Sha512 []byte `json:"sha512"`
	Size   uint64 `json:"size"`
}

// UpgradeReq system upgrade request
type UpgradeReq struct {
	ImageVersion uint64    `json:"imageVersion"`
	ImageInfo    ImageInfo `json:"imageInfo"`
}

// RevertReq system revert request
type RevertReq struct {
	ImageVersion uint64 `json:"imageVersion"`
}

// StatusReq get system status request
type StatusReq struct {
}

// StatusRsp status response message
type StatusRsp struct {
	Operation        string `json:"operation"`       // upgrade, revert
	Status           string `json:"status"`          // success, failed, inProgress
	Error            string `json:"error,omitempty"` // error message if status failed
	RequestedVersion uint64 `json:"requestedVersion"`
	CurrentVersion   uint64 `json:"currentVersion"`
}

// CreateKeysReq creates key pair request
type CreateKeysReq struct {
	Type     string `json:"type"`
	SystemID string `json:"systemID"`
	Password string `json:"password"`
}

// CreateKeysRsp creates key pair response
type CreateKeysRsp struct {
	Type  string `json:"type"`
	Csr   string `json:"csr"`
	Error string `json:"error,omitempty"`
}

// ApplyCertReq apply certificate request
type ApplyCertReq struct {
	Type string `json:"type"`
	Crt  string `json:"crt"`
}

// ApplyCertRsp apply certificate response
type ApplyCertRsp struct {
	Type   string `json:"type"`
	CrtURL string `json:"crtUrl"`
	Error  string `json:"error,omitempty"`
}

// GetCertReq get certificate request
type GetCertReq struct {
	Type   string `json:"type"`
	Issuer []byte `json:"issuer,omitempty"`
	Serial string `json:"serial,omitempty"`
}

// GetCertRsp get certificate response
type GetCertRsp struct {
	Type   string `json:"type"`
	CrtURL string `json:"crtUrl"`
	KeyURL string `json:"keyUrl"`
	Error  string `json:"error,omitempty"`
}
