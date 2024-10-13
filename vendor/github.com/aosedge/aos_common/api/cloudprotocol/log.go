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

// Log message types.
const (
	RequestLogMessageType = "requestLog"
	PushLogMessageType    = "pushLog"
)

// Log types.
const (
	SystemLog  = "systemLog"
	ServiceLog = "serviceLog"
	CrashLog   = "crashLog"
)

// Log statuses.
const (
	LogStatusOk     = "ok"
	LogStatusError  = "error"
	LogStatusEmpty  = "empty"
	LogStatusAbsent = "absent"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// LogUploadOptions request log message.
type LogUploadOptions struct {
	Type           string     `json:"type"`
	URL            string     `json:"url"`
	BearerToken    string     `json:"bearerToken"`
	BearerTokenTTL *time.Time `json:"bearerTokenTtl"`
}

// LogFilter request log message.
type LogFilter struct {
	From          *time.Time        `json:"from"`
	Till          *time.Time        `json:"till"`
	NodeIDs       []string          `json:"nodeIds,omitempty"`
	UploadOptions *LogUploadOptions `json:"uploadOptions,omitempty"`
	InstanceFilter
}

// RequestLog request log message.
type RequestLog struct {
	MessageType string    `json:"messageType"`
	LogID       string    `json:"logId"`
	LogType     string    `json:"logType"`
	Filter      LogFilter `json:"filter"`
}

// PushLog push service log structure.
type PushLog struct {
	MessageType string     `json:"messageType"`
	NodeID      string     `json:"nodeId"`
	LogID       string     `json:"logId"`
	PartsCount  uint64     `json:"partsCount,omitempty"`
	Part        uint64     `json:"part,omitempty"`
	Content     []byte     `json:"content,omitempty"`
	Status      string     `json:"status"`
	ErrorInfo   *ErrorInfo `json:"errorInfo,omitempty"`
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

func NewInstanceFilter(serviceID, subjectID string, instance int64) (filter InstanceFilter) {
	if serviceID != "" {
		filter.ServiceID = &serviceID
	}

	if subjectID != "" {
		filter.SubjectID = &subjectID
	}

	if instance != -1 {
		localInstance := (uint64)(instance)

		filter.Instance = &localInstance
	}

	return filter
}
