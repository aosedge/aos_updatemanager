package grub

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const grubEnvHeader = "# GRUB Environment Block"
const grubEnvSize = 1024
const preallocatedDataSize = 10

/*******************************************************************************
 * Types
 ******************************************************************************/

// Instance grub instance
type Instance struct {
	file     *os.File
	data     []envVariable
	modified bool
	size     int
}

type envVariable struct {
	name  string
	value string
}

/*******************************************************************************
 * Vars
 ******************************************************************************/

/*******************************************************************************
 * Public
 ******************************************************************************/

// New creates new grub instance
func New(envFile string) (instance *Instance, err error) {
	log.WithField("fileName", envFile).Debug("Create GRUB instance")

	instance = &Instance{
		data: make([]envVariable, 0, preallocatedDataSize),
	}

	if instance.file, err = os.OpenFile(envFile, os.O_RDWR, 0666); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(instance.file)

	headerChecked := false

	for scanner.Scan() {
		line := scanner.Text()

		if !headerChecked {
			if line != grubEnvHeader {
				return nil, errors.New("invalid env header")
			}

			instance.size += len(line) + 1

			headerChecked = true
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		data := strings.Split(line, "=")
		if len(data) != 2 {
			return nil, errors.New("invalid env format")
		}

		variable := envVariable{strings.ReplaceAll(data[0], "\\\\", "\\"), strings.ReplaceAll(data[1], "\\\\", "\\")}

		log.WithFields(log.Fields{"name": variable.name, "value": variable.value}).Debug("GRUB env variable")

		instance.data = append(instance.data, variable)
		instance.size += len(line) + 1
	}

	if !headerChecked {
		return nil, errors.New("invalid env format")
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	log.WithField("size", grubEnvSize-instance.size).Debug("Grub env free size")

	return instance, nil
}

// GetVariable returns env variable
func (instance *Instance) GetVariable(name string) (value string, err error) {
	for _, variable := range instance.data {
		if variable.name == name {
			log.WithFields(log.Fields{"name": variable.name, "value": variable.value}).Debug("Get GRUB env variable")

			return variable.value, nil
		}
	}

	return "", fmt.Errorf("variable %s not found", name)
}

// GetVariables returns all env variables
func (instance *Instance) GetVariables() (vars map[string]string, err error) {
	vars = make(map[string]string)

	for _, variable := range instance.data {
		vars[variable.name] = variable.value
	}

	return vars, nil
}

// SetVariable sets env variable
func (instance *Instance) SetVariable(name string, value string) (err error) {
	log.WithFields(log.Fields{"name": name, "value": value}).Debug("Set GRUB env variable")

	for i, variable := range instance.data {
		if variable.name == name {
			size := (len(value) + strings.Count(value, "\\")) -
				(len(variable.value) + strings.Count(variable.value, "\\"))

			if grubEnvSize-instance.size < size {
				return errors.New("not enough space")
			}

			instance.data[i].value = value
			instance.size = instance.size + size
			instance.modified = true

			return nil
		}
	}

	size := len(name) + strings.Count(name, "\\") + 1 + len(value) + strings.Count(value, "\\") + 1

	if grubEnvSize-instance.size < size {
		return errors.New("not enough space")
	}

	instance.data = append(instance.data, envVariable{name, value})
	instance.size = instance.size + size
	instance.modified = true

	return nil
}

// UnsetVariable unsets env variable
func (instance *Instance) UnsetVariable(name string) (err error) {
	log.WithFields(log.Fields{"name": name}).Debug("Unset GRUB env variable")

	for i, variable := range instance.data {
		if variable.name == name {
			size := len(variable.name) + strings.Count(variable.name, "\\") + 1 +
				len(variable.value) + strings.Count(variable.value, "\\") + 1

			if size > instance.size {
				return errors.New("wrong env size")
			}

			instance.data = append(instance.data[:i], instance.data[i+1:]...)
			instance.size = instance.size - size
			instance.modified = true

			return nil
		}
	}

	return nil
}

// Store stores modified variables
func (instance *Instance) Store() (err error) {
	if !instance.modified {
		return nil
	}

	log.Debug("Store GRUB instance")

	if _, err = instance.file.Seek(0, 0); err != nil {
		return err
	}

	writer := bufio.NewWriter(instance.file)

	size := 0

	n, err := fmt.Fprintln(writer, grubEnvHeader)
	if err != nil {
		return err
	}

	size = size + n

	for _, variable := range instance.data {
		n, err := fmt.Fprintf(writer, "%s=%s\n", strings.ReplaceAll(variable.name, "\\", "\\\\"),
			strings.ReplaceAll(variable.value, "\\", "\\\\"))
		if err != nil {
			return err
		}

		size = size + n
	}

	if size > grubEnvSize {
		return fmt.Errorf("invalid env size: %d", size)
	}

	if _, err = fmt.Fprint(writer, strings.Repeat("#", grubEnvSize-size)); err != nil {
		return err
	}

	if err = writer.Flush(); err != nil {
		return err
	}

	if err = instance.file.Sync(); err != nil {
		return err
	}

	instance.modified = false

	return nil
}

// Close closes grub instance
func (instance *Instance) Close() (err error) {
	defer instance.file.Close()

	log.Debug("Close GRUB instance")

	if err = instance.Store(); err != nil {
		return err
	}

	return nil
}
