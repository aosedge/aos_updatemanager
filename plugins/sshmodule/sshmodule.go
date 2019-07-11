package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/updatehandler"
)

/*******************************************************************************
 * Types
 ******************************************************************************/

// SSHModule SSH module
type SSHModule struct {
	id string
	sync.Mutex
	config moduleConfig
}

type moduleConfig struct {
	Host     string
	User     string
	Password string
	DestPath string
	Commands []string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// NewModule creates test module instance
func NewModule(id string, configJSON []byte) (module updatehandler.Module, err error) {
	log.WithField("id", id).Info("Create SSH module")

	sshModule := &SSHModule{id: id}

	if err = json.Unmarshal(configJSON, &sshModule.config); err != nil {
		return nil, err
	}

	return sshModule, nil
}

// Close closes test module
func (module *SSHModule) Close() {
	log.WithField("id", module.id).Info("Close SSH module")
}

// GetID returns module ID
func (module *SSHModule) GetID() (id string) {
	module.Lock()
	defer module.Unlock()

	return module.id
}

// Upgrade upgrade module
func (module *SSHModule) Upgrade(fileName string) (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithFields(log.Fields{
		"id":       module.id,
		"fileName": fileName}).Info("Upgrade")

	// Create SSH connection
	config := &ssh.ClientConfig{
		User:            module.config.User,
		Auth:            []ssh.AuthMethod{ssh.Password(module.config.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey()}

	client, err := ssh.Dial("tcp", module.config.Host, config)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	log.WithFields(log.Fields{"src": fileName, "dst": module.config.DestPath}).Debug("Copy file")

	// Copy file to the remote DestDir
	if err = scp.CopyPath(fileName, module.config.DestPath, session); err != nil {
		return err
	}

	if err = module.runCommands(client); err != nil {
		return err
	}

	return nil
}

// Revert revert module
func (module *SSHModule) Revert() (err error) {
	module.Lock()
	defer module.Unlock()

	log.WithField("id", module.id).Info("Revert")

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func (module *SSHModule) runCommands(client *ssh.Client) (err error) {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err = session.Shell(); err != nil {
		return err
	}

	for _, command := range module.config.Commands {
		log.WithField("command", command).Debug("SSH command")

		if _, err = fmt.Fprintf(stdin, "%s\n", command); err != nil {
			return err
		}
	}

	if _, err = fmt.Fprintf(stdin, "%s\n", "exit"); err != nil {
		return err
	}

	if err = session.Wait(); err != nil {
		return err
	}

	return nil
}
