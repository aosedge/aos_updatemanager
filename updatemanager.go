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

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/iamclient"
	"github.com/aosedge/aos_common/utils/cryptutils"
	"github.com/coreos/go-systemd/daemon"
	"github.com/coreos/go-systemd/journal"
	log "github.com/sirupsen/logrus"

	"github.com/aosedge/aos_updatemanager/config"
	"github.com/aosedge/aos_updatemanager/database"
	"github.com/aosedge/aos_updatemanager/umclient"
	"github.com/aosedge/aos_updatemanager/updatehandler"
	_ "github.com/aosedge/aos_updatemanager/updatemodules"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const dbFileName = "updatemanager.db"

/*******************************************************************************
 * Vars
 ******************************************************************************/

// GitSummary provided by govvv at compile-time.
var GitSummary string //nolint:gochecknoglobals

/*******************************************************************************
 * Types
 ******************************************************************************/

type journalHook struct {
	severityMap map[log.Level]journal.Priority
}

type updateManager struct {
	db            *database.Database
	updater       *updatehandler.Handler
	client        *umclient.Client
	cryptoContext *cryptutils.CryptoContext
	iam           *iamclient.Client
}

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true,
	})
}

/*******************************************************************************
 * Update manager
 ******************************************************************************/

func newUpdateManager(cfg *config.Config) (um *updateManager, err error) {
	um = &updateManager{}

	defer func() {
		if err != nil {
			um.close()
			um = nil
		}
	}()

	// Create DB
	dbFile := path.Join(cfg.WorkingDir, dbFileName)

	um.db, err = database.New(dbFile, cfg.Migration.MigrationPath, cfg.Migration.MergedMigrationPath)
	if err != nil {
		if strings.Contains(err.Error(), database.ErrMigrationFailedStr) {
			log.Warning("Unable to perform db migration")

			cleanup(dbFile)

			um.db, err = database.New(dbFile, cfg.Migration.MigrationPath, cfg.Migration.MergedMigrationPath)
		}

		if err != nil {
			return um, aoserrors.Wrap(err)
		}
	}

	um.updater, err = updatehandler.New(cfg, um.db, um.db)
	if err != nil {
		return um, aoserrors.Wrap(err)
	}

	um.cryptoContext, err = cryptutils.NewCryptoContext(cfg.CACert)
	if err != nil {
		return um, aoserrors.Wrap(err)
	}

	um.iam, err = iamclient.New(cfg.IAMPublicServerURL, "", "", nil, um.cryptoContext, false)
	if err != nil {
		return um, aoserrors.Wrap(err)
	}

	um.client, err = umclient.New(cfg, um.updater, um.iam, um.cryptoContext, false)
	if err != nil {
		return um, aoserrors.Wrap(err)
	}

	return um, nil
}

func (um *updateManager) close() {
	if um.db != nil {
		um.db.Close()
	}

	if um.updater != nil {
		um.updater.Close()
	}

	if um.client != nil {
		um.client.Close()
	}

	if um.cryptoContext != nil {
		um.cryptoContext.Close()
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func cleanup(dbFile string) {
	log.Debug("System cleanup")

	log.WithField("file", dbFile).Debug("Delete DB file")

	if err := os.RemoveAll(dbFile); err != nil {
		log.Fatalf("Can't cleanup database: %s", aoserrors.Wrap(err))
	}
}

func newJournalHook() (hook *journalHook) {
	hook = &journalHook{
		severityMap: map[log.Level]journal.Priority{
			log.DebugLevel: journal.PriDebug,
			log.InfoLevel:  journal.PriInfo,
			log.WarnLevel:  journal.PriWarning,
			log.ErrorLevel: journal.PriErr,
			log.FatalLevel: journal.PriCrit,
			log.PanicLevel: journal.PriEmerg,
		},
	}

	return hook
}

func (hook *journalHook) Fire(entry *log.Entry) (err error) {
	if entry == nil {
		return aoserrors.New("log entry is nil")
	}

	logMessage, err := entry.String()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	err = journal.Print(hook.severityMap[entry.Level], logMessage)

	return aoserrors.Wrap(err)
}

func (hook *journalHook) Levels() []log.Level {
	return []log.Level{
		log.PanicLevel,
		log.FatalLevel,
		log.ErrorLevel,
		log.WarnLevel,
		log.InfoLevel,
		log.DebugLevel,
	}
}

/*******************************************************************************
 *  Main
 ******************************************************************************/

func main() {
	// Initialize command line flags
	configFile := flag.String("c", "aos_updatemanager.cfg", "path to config file")
	strLogLevel := flag.String("v", "info", `log level: "debug", "info", "warn", "error", "fatal", "panic"`)
	useJournal := flag.Bool("j", false, "output logs to systemd journal")
	showVersion := flag.Bool("version", false, `show update manager version`)

	flag.Parse()

	// Show version
	if *showVersion {
		fmt.Printf("Version: %s\n", GitSummary) //nolint:forbidigo // logs are't initialized
		return
	}

	if *useJournal {
		log.AddHook(newJournalHook())
		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(os.Stdout)
	}

	// Set log level
	logLevel, err := log.ParseLevel(*strLogLevel)
	if err != nil {
		log.Fatalf("Error: %s", aoserrors.Wrap(err))
	}

	log.SetLevel(logLevel)
	log.WithFields(log.Fields{"configFile": *configFile, "version": GitSummary}).Info("Start update manager")

	cfg, err := config.New(*configFile)
	if err != nil {
		log.Fatalf("Can' open config file: %s", aoserrors.Wrap(err))
	}

	um, err := newUpdateManager(cfg)
	if err != nil {
		log.Fatalf("Can't create update manager: %s", err)
	}

	defer um.close()

	// Notify systemd
	if _, err = daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		log.Errorf("Can't notify systemd: %s", err)
	}

	// Handle SIGTERM
	terminateChannel := make(chan os.Signal, 1)
	signal.Notify(terminateChannel, os.Interrupt, syscall.SIGTERM)

	<-terminateChannel
}
