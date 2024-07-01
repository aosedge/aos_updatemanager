// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2023 Renesas Electronics Corporation.
// Copyright (C) 2023 EPAM Systems, Inc.
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

package bootparams

import (
	"os"
	"regexp"
	"strings"

	"github.com/aosedge/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

type Handler struct {
	params map[string]string
}

/***********************************************************************************************************************
 * consts
 **********************************************************************************************************************/

const (
	autoMode     = "auto"
	bootArgsMode = "bootargs"
	manualMode   = "manual"
)

const (
	rootParam            = "root"
	aosUpdateBootPartA   = "aosupdate.boot.parta"
	aosUpdateBootPartB   = "aosupdate.boot.partb"
	aosUpdateBootEnvPart = "aosupdate.boot.envPart"
)

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

// BootParamsPath path to boot params file.
var BootParamsPath = "/proc/cmdline" //nolint:gochecknoglobals // Used in unit tests to override path

var rootExp = regexp.MustCompile("[[:digit:]]+$")

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new parser instance.
func New() (*Handler, error) {
	data, err := os.ReadFile(BootParamsPath)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	paramsList := strings.Fields(string(data))

	handler := &Handler{params: make(map[string]string)}

	for _, param := range paramsList {
		nameValue := strings.SplitN(param, "=", 2)

		if len(nameValue) > 0 {
			name := nameValue[0]
			value := ""

			if len(nameValue) > 1 {
				value = nameValue[1]
			}

			handler.params[name] = value
		}
	}

	return handler, nil
}

// GetBootParts returns boot partitions based on detect mode and partition suffixes.
func (handler *Handler) GetBootParts(mode string, partSuffixes []string) ([]string, error) {
	switch strings.ToLower(mode) {
	case autoMode:
		rootValue, ok := handler.params[rootParam]
		if !ok {
			return nil, aoserrors.Errorf("no %s parameter found", rootParam)
		}

		if !rootExp.MatchString(rootValue) {
			return nil, aoserrors.New("can't define root device")
		}

		result := make([]string, len(partSuffixes))

		for i, suffix := range partSuffixes {
			result[i] = rootExp.ReplaceAllString(rootValue, suffix)
		}

		return result, nil

	case bootArgsMode:
		partA, ok := handler.params[aosUpdateBootPartA]
		if !ok {
			return nil, aoserrors.Errorf("no %s parameter found", aosUpdateBootPartA)
		}

		partB, ok := handler.params[aosUpdateBootPartB]
		if !ok {
			return nil, aoserrors.Errorf("no %s parameter found", aosUpdateBootPartB)
		}

		if partA == "" {
			return nil, aoserrors.Errorf("%s parameter empty", aosUpdateBootPartA)
		}

		if partB == "" {
			return nil, aoserrors.Errorf("%s parameter empty", aosUpdateBootPartB)
		}

		return []string{partA, partB}, nil

	case manualMode:
		result := make([]string, len(partSuffixes))

		copy(result, partSuffixes)

		return result, nil

	default:
		return nil, aoserrors.Errorf("unsupported mode: %s", mode)
	}
}

// GetEnvPart returns boot environment partition based on detect mode and partition suffix.
func (handler *Handler) GetEnvPart(mode string, partSuffix string) (string, error) {
	switch strings.ToLower(mode) {
	case autoMode:
		rootValue, ok := handler.params[rootParam]
		if !ok {
			return "", aoserrors.Errorf("no %s parameter found", rootParam)
		}

		if !rootExp.MatchString(rootValue) {
			return "", aoserrors.New("can't define root device")
		}

		return rootExp.ReplaceAllString(rootValue, partSuffix), nil

	case bootArgsMode:
		envPart, ok := handler.params[aosUpdateBootEnvPart]
		if !ok {
			return "", aoserrors.Errorf("no %s parameter found", aosUpdateBootEnvPart)
		}

		if envPart == "" {
			return "", aoserrors.Errorf("%s parameter empty", aosUpdateBootEnvPart)
		}

		return envPart, nil

	case manualMode:
		return partSuffix, nil

	default:
		return "", aoserrors.Errorf("unsupported mode: %s", mode)
	}
}
