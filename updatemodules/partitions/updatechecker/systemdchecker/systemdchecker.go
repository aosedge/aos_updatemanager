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

package systemdchecker

import (
	"context"
	"sync"
	"time"

	"github.com/aoscloud/aos_common/aoserrors"
	"github.com/coreos/go-systemd/v22/dbus"
	log "github.com/sirupsen/logrus"

	"aos_updatemanager/config"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const defaultTimeout = 30 * time.Second

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// Config watch services configuration
type Config struct {
	SystemServices []string        `json:"systemServices"`
	UserServices   []string        `json:"userServices"`
	Timeout        config.Duration `json:"timeout"`
}

// Checker systemd checker instance
type Checker struct {
	cfg Config
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// New creates new systemd checker instance
func New(cfg Config) (checker *Checker) {
	if cfg.Timeout.Duration == 0 {
		cfg.Timeout.Duration = defaultTimeout
	}

	return &Checker{cfg: cfg}
}

// Check performs update validation
func (checker *Checker) Check() (err error) {
	var (
		wg                 sync.WaitGroup
		systemErr, userErr error
	)

	if len(checker.cfg.SystemServices) != 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()
			systemErr = aoserrors.Wrap(watchServices(dbus.NewSystemConnectionContext,
				checker.cfg.SystemServices, checker.cfg.Timeout.Duration))
		}()
	}

	if len(checker.cfg.UserServices) != 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()
			userErr = aoserrors.Wrap(watchServices(dbus.NewUserConnectionContext, checker.cfg.UserServices, checker.cfg.Timeout.Duration))
		}()
	}

	wg.Wait()

	if systemErr != nil {
		return aoserrors.Wrap(systemErr)
	}

	if userErr != nil {
		return aoserrors.Wrap(userErr)
	}

	return nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func watchServices(createConnection func(context.Context) (*dbus.Conn, error), services []string, timeout time.Duration) (err error) {
	systemd, err := createConnection(context.Background())
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer systemd.Close()

	subscriptionSet := systemd.NewSubscriptionSet()

	for _, service := range services {
		subscriptionSet.Add(service)
	}

	statusChannel, errorChannel := subscriptionSet.Subscribe()
	timeoutChannel := time.After(timeout)

	for {
		select {
		case serviceStatuses := <-statusChannel:
			allActivated := true

			for service, status := range serviceStatuses {
				activeState := "failed"

				if status != nil {
					activeState = status.ActiveState
				}

				log.WithFields(log.Fields{"service": service, "state": activeState}).Debug("Watched service state changed")

				if activeState != "active" {
					allActivated = false
				}

				if activeState == "failed" {
					return aoserrors.Errorf("watched service %s failed", service)
				}
			}

			if allActivated {
				return nil
			}

		case err := <-errorChannel:
			return aoserrors.Wrap(err)

		case <-timeoutChannel:
			return aoserrors.New("watch services timeout")
		}
	}
}
