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

package contextreader

import (
	"context"
	"io"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

type ContextReader struct {
	ctx    context.Context
	reader io.Reader
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new context reader
func New(ctx context.Context, reader io.Reader) (contextReader io.Reader) {
	return &ContextReader{
		ctx:    ctx,
		reader: reader,
	}
}

func (contextReader *ContextReader) Read(p []byte) (n int, err error) {
	select {
	case <-contextReader.ctx.Done():
		return 0, contextReader.ctx.Err()

	default:
		return contextReader.reader.Read(p)
	}
}
