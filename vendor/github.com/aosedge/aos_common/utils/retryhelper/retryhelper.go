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

package retryhelper

import (
	"context"
	"time"

	"github.com/aosedge/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	defaultMaxTry        = 3
	defaultRetryDelay    = 1 * time.Second
	defaultMaxRetryDelay = 1 * time.Minute
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// Retry performs operation defined number of times with configured delay.
func Retry(
	ctx context.Context, retryFunc func() error, retryCbk func(retryCount int, delay time.Duration, err error),
	maxTry int, delay, maxDelay time.Duration,
) (err error) {
	try := 1

	for {
		if err = retryFunc(); err == nil {
			return nil
		}

		if try < maxTry || maxTry == 0 {
			if ctx.Err() == nil && retryCbk != nil {
				retryCbk(try, delay, err)
			}

			select {
			case <-ctx.Done():
				return aoserrors.Wrap(ctx.Err())

			case <-time.After(delay):
			}

			delay *= 2

			if maxDelay != 0 && delay > maxDelay {
				delay = maxDelay
			}

			try++

			continue
		}

		break
	}

	return aoserrors.Wrap(err)
}

// DefaultRetry performs operation default number of times with default delay.
func DefaultRetry(ctx context.Context, retryFunc func() error) (err error) {
	return Retry(ctx, retryFunc, nil, defaultMaxTry, defaultRetryDelay, 0)
}

// DefaultInfinitRetry performs operation default number of times with default delay.
func DefaultInfinitRetry(ctx context.Context, retryFunc func() error,
	retryCbk func(retryCount int, delay time.Duration, err error),
) (err error) {
	return Retry(ctx, retryFunc, retryCbk, 0, defaultRetryDelay, defaultMaxRetryDelay)
}
