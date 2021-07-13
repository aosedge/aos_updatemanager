// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Renesas Inc.
// Copyright 2021 EPAM Systems Inc.
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

package aoserrors

import (
	"fmt"
	"runtime"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// Custom error type with caller info
type tracedError struct {
	msg string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

func New(message string) (err error) {
	pc, _, line, ok := runtime.Caller(1)
	callerInfo := formatCallerInfoString(pc, line, ok)

	return &tracedError{
		msg: fmt.Sprintf("%s %s", message, callerInfo),
	}
}

func Errorf(format string, args ...interface{}) (err error) {
	pc, _, line, ok := runtime.Caller(1)
	callerInfo := formatCallerInfoString(pc, line, ok)
	message := fmt.Sprintf(format, args...)

	return &tracedError{
		msg: fmt.Sprintf("%s %s", message, callerInfo),
	}
}

func (err *tracedError) Error() (str string) {
	return err.msg
}

func Wrap(err error) (retErr error) {
	if err == nil {
		return nil
	}

	if _, ok := err.(*tracedError); ok {
		return err
	}

	pc, _, line, ok := runtime.Caller(1)
	callerInfo := formatCallerInfoString(pc, line, ok)
	
	return &tracedError{
	    msg: fmt.Sprintf("%s %s", err, callerInfo),
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func formatCallerInfoString(pc uintptr, line int, ok bool) (str string) {
	if ok {
		return fmt.Sprintf("[%s:%d]", runtime.FuncForPC(pc).Name(), line)
	} else {
		return "[unknown:???]"
	}
}
