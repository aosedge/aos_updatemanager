// SPDX-License-Identifier: Apache-2.0
//
// Copyright (C) 2024 Renesas Electronics Corporation.
// Copyright (C) 2024 EPAM Systems, Inc.
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

package idprovider_test

import (
	"os"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	idprovider "github.com/aosedge/aos_updatemanager/utils"
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	ret := m.Run()

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetID(t *testing.T) {
	id, err := idprovider.CreateID("test")
	if err != nil {
		t.Fatalf("Can't create id: %s", err)
	}

	if id == "" {
		t.Fatalf("ID is empty")
	}

	if !strings.HasPrefix(id, "test") {
		t.Fatalf("ID '%s' doesn't start with 'test'", id)
	}
}
