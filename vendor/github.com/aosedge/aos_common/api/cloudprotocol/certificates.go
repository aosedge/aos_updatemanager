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

// Certificate message types.
const (
	RenewCertsNotificationMessageType       = "renewCertificatesNotification"
	IssuedUnitCertsMessageType              = "issuedUnitCertificates"
	IssueUnitCertsMessageType               = "issueUnitCertificates"
	InstallUnitCertsConfirmationMessageType = "installUnitCertificatesConfirmation"
)

// UnitSecretVersion specifies supported version of UnitSecret message.
const UnitSecretVersion = "2.0.0"

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// IssuedCertData issued unit certificate data.
type IssuedCertData struct {
	Type             string `json:"type"`
	NodeID           string `json:"nodeId,omitempty"`
	CertificateChain string `json:"certificateChain"`
}

// InstallCertData install certificate data.
type InstallCertData struct {
	Type        string `json:"type"`
	NodeID      string `json:"nodeId,omitempty"`
	Serial      string `json:"serial"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
}

// RenewCertData renew certificate data.
type RenewCertData struct {
	Type      string    `json:"type"`
	NodeID    string    `json:"nodeId,omitempty"`
	Serial    string    `json:"serial"`
	ValidTill time.Time `json:"validTill"`
}

// UnitSecrets keeps secrets for nodes.
type UnitSecrets struct {
	Version string            `json:"version"`
	Nodes   map[string]string `json:"nodes"`
}

// IssueCertData issue certificate data.
type IssueCertData struct {
	Type   string `json:"type"`
	NodeID string `json:"nodeId,omitempty"`
	Csr    string `json:"csr"`
}

// RenewCertsNotification renew certificate notification from cloud with pwd.
type RenewCertsNotification struct {
	MessageType  string          `json:"messageType"`
	Certificates []RenewCertData `json:"certificates"`
	UnitSecrets  UnitSecrets     `json:"unitSecrets"`
}

// IssuedUnitCerts issued unit certificates info.
type IssuedUnitCerts struct {
	MessageType  string           `json:"messageType"`
	Certificates []IssuedCertData `json:"certificates"`
}

// IssueUnitCerts issue unit certificates request.
type IssueUnitCerts struct {
	MessageType string          `json:"messageType"`
	Requests    []IssueCertData `json:"requests"`
}

// InstallUnitCertsConfirmation install unit certificates confirmation.
type InstallUnitCertsConfirmation struct {
	MessageType  string            `json:"messageType"`
	Certificates []InstallCertData `json:"certificates"`
}
