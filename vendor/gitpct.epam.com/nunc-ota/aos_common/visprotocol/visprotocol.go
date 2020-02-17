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

package visprotocol

/*******************************************************************************
 * Consts
 ******************************************************************************/

// VIS actions
const (
	ActionGet            = "get"
	ActionSet            = "set"
	ActionAuth           = "authorize"
	ActionSubscribe      = "subscribe"
	ActionUnsubscribe    = "unsubscribe"
	ActionUnsubscribeAll = "unsubscribeAll"
	ActionSubscription   = "subscription"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// MessageHeader VIS message header
type MessageHeader struct {
	Action    string `json:"action"`
	RequestID string `json:"requestId"`
}

// ErrorInfo VIS error info
type ErrorInfo struct {
	Number  int    `json:"number"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// Tokens VIS authorize tokens
type Tokens struct {
	Authorization    string `json:"authorization,omitempty"`
	WwwVehicleDevice string `json:"www-vehicle-device,omitempty"`
}

// AuthRequest VIS authorize request
type AuthRequest struct {
	MessageHeader
	Tokens Tokens `json:"tokens"`
}

// AuthResponse VIS authorize success response
type AuthResponse struct {
	MessageHeader
	Error *ErrorInfo `json:"error,omitempty"`
	TTL   int64      `json:"TTL,omitempty"`
}

// GetRequest VIS get request
type GetRequest struct {
	MessageHeader
	Path string `json:"path"`
}

// GetResponse VIS get success response
type GetResponse struct {
	MessageHeader
	Error     *ErrorInfo  `json:"error,omitempty"`
	Value     interface{} `json:"value,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// SetRequest VIS set request
type SetRequest struct {
	MessageHeader
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

// SetResponse VIS set success response
type SetResponse struct {
	MessageHeader
	Error     *ErrorInfo `json:"error,omitempty"`
	Timestamp int64      `json:"timestamp,omitempty"`
}

// SubscribeRequest VIS subscribe request
type SubscribeRequest struct {
	MessageHeader
	Path    string `json:"path"`
	Filters string `json:"filters,omitempty"` //TODO: will be implemented later
}

// SubscribeResponse VIS subscribe success response
type SubscribeResponse struct {
	MessageHeader
	Error          *ErrorInfo `json:"error,omitempty"`
	SubscriptionID string     `json:"subscriptionId,omitempty"`
	Timestamp      int64      `json:"timestamp"`
}

// SubscriptionNotification VIS subscription notification
type SubscriptionNotification struct {
	Error          *ErrorInfo  `json:"error,omitempty"`
	Action         string      `json:"action"`
	SubscriptionID string      `json:"subscriptionId"`
	Value          interface{} `json:"value,omitempty"`
	Timestamp      int64       `json:"timestamp"`
}

// UnsubscribeRequest VIS unsubscribe request
type UnsubscribeRequest struct {
	MessageHeader
	SubscriptionID string `json:"subscriptionId"`
}

// UnsubscribeResponse VIS unsubscribe success response
type UnsubscribeResponse struct {
	MessageHeader
	Error          *ErrorInfo `json:"error,omitempty"`
	SubscriptionID string     `json:"subscriptionId"`
	Timestamp      int64      `json:"timestamp"`
}

// UnsubscribeAllRequest VIS unsubscribe all request
type UnsubscribeAllRequest struct {
	MessageHeader
}

// UnsubscribeAllResponse VIS unsubscribe all success response
type UnsubscribeAllResponse struct {
	MessageHeader
	Error     *ErrorInfo `json:"error,omitempty"`
	Timestamp int64      `json:"timestamp"`
}
