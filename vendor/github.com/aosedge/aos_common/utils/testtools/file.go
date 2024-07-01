// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2022 EPAM Systems, Inc.
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

package testtools

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"os"

	"github.com/aosedge/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// CompareFiles compares files.
func CompareFiles(dst, src string) (err error) {
	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_RDONLY, 0)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	srcSha256 := sha256.New()
	dstSha256 := sha256.New()

	srcSize, err := srcFile.Seek(0, io.SeekEnd)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	dstSize, err := dstFile.Seek(0, io.SeekEnd)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if srcSize != dstSize {
		return aoserrors.New("size mismatch")
	}

	if _, err = srcFile.Seek(0, io.SeekStart); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err = dstFile.Seek(0, io.SeekStart); err != nil {
		return aoserrors.Wrap(err)
	}

	if _, err := io.Copy(srcSha256, srcFile); err != nil && errors.Is(err, io.EOF) {
		return aoserrors.Wrap(err)
	}

	if _, err := io.Copy(dstSha256, dstFile); err != nil && errors.Is(err, io.EOF) {
		return aoserrors.Wrap(err)
	}

	if !bytes.Equal(srcSha256.Sum(nil), dstSha256.Sum(nil)) {
		return aoserrors.New("data mismatch")
	}

	return nil
}
