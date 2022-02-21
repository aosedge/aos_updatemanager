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

package aoserrors

import (
	"errors"
	"fmt"
	"runtime"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const callerLevel = 2

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Error Aos error type.
type Error struct {
	pc   uintptr
	line int
	err  error
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new Aos error from string message.
func New(message string) error {
	return createAosError(errors.New(message)) // nolint:goerr113 // convert to Aos error
}

// Errorf creates new formatted Aos error.
func Errorf(format string, args ...interface{}) error {
	return createAosError(fmt.Errorf(format, args...)) // nolint:goerr113 // convert to Aos error
}

// Wrap wraps existing error.
func Wrap(fromErr error) error {
	if fromErr == nil {
		return nil
	}

	if errors.As(fromErr, new(*Error)) {
		return fromErr
	}

	return createAosError(fromErr)
}

// Error returns Aos error message.
func (aosErr *Error) Error() string {
	f := runtime.FuncForPC(aosErr.pc)
	if f == nil {
		return "[unknown:???]"
	}

	return fmt.Sprintf("%s [%s:%d]", aosErr.err.Error(), f.Name(), aosErr.line)
}

// Unwrap unwraps error.
func (aosErr *Error) Unwrap() error {
	return aosErr.err
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func createAosError(fromErr error) *Error {
	aosErr := &Error{err: fromErr}

	aosErr.pc, _, aosErr.line, _ = runtime.Caller(callerLevel)

	return aosErr
}
