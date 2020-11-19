// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2020 Renesas Inc.
// Copyright 2020 EPAM Systems Inc.
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

package efi

/*
 #cgo pkg-config: efivar efiboot

#include <efivar.h>
#include <efiboot-loadopt.h>
#include <efiboot-creator.h>

static ssize_t efi_generate_file_device_path_from_esp_go(
	uint8_t *buf,
	ssize_t size,
	const char *devpath,
	int partition,
	const char *relpath,
	uint32_t options,
	uint32_t eddDeviceNum)
{
	return efi_generate_file_device_path_from_esp(buf, size, devpath, partition, relpath, options, eddDeviceNum);
}
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gitpct.epam.com/epmd-aepr/aos_common/partition"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const preallocatedItemSize = 10

const (
	hdFormatPCAT = iota + 1
	hdFormatGPT
)

const efiBootAbbrevHD = 2
const eddDefaultDevice = 0x80

const (
	hdSignatureNone = iota
	hdSignatureMBR
	hdSignatureGUID
)

const (
	bootItemNamePattern = "^Boot[[:xdigit:]]{4}$"
	bootItemIDPattern   = "[[:xdigit:]]{4}$"
)

const (
	efiBootOrderName   = "BootOrder"
	efiBootCurrentName = "BootCurrent"
	efiBootNextName    = "BootNext"
)

const (
	efiGlobalGUID = "8be4df61-93ca-11d2-aa0d-00e098032b8c"
)

const loadOptionActive = 0x00000001

const writeAttribute = 0644

/*******************************************************************************
 * Vars
 ******************************************************************************/

// ErrNotFound efi var not exist error
var ErrNotFound = errors.New("EFI var not found")

/*******************************************************************************
 * Types
 ******************************************************************************/

// Instance boot instance
type Instance struct {
	bootItems []bootItem
}

type bootItem struct {
	id          uint16
	name        string
	attributes  uint32
	description string
	data        []byte
}

type hdData struct {
	partNumber    uint32
	start         uint64
	size          uint64
	signature     [16]byte
	format        uint8
	signatureType uint8
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// New returns new EFI instance
func New() (instance *Instance, err error) {
	if rc := C.efi_variables_supported(); rc == 0 {
		return nil, errors.New("EFI variables are not supported on this system")
	}

	instance = &Instance{bootItems: make([]bootItem, 0, preallocatedItemSize)}

	if err = instance.readBootItems(); err != nil {
		return nil, err
	}

	return instance, nil
}

// GetBootByPartUUID returns boot item by PARTUUID
func (instance *Instance) GetBootByPartUUID(partUUID uuid.UUID) (id uint16, err error) {
	for _, item := range instance.bootItems {
		if item.data == nil {
			continue
		}

		efiLoadOption := (*C.efi_load_option)(C.CBytes(item.data))
		pathLen := C.efi_loadopt_pathlen(efiLoadOption, C.ssize_t(len(item.data)))
		dpData := C.efi_loadopt_path(efiLoadOption, C.ssize_t(len(item.data)))

		dps, err := parseDP(C.GoBytes(unsafe.Pointer(dpData), C.int(pathLen)))
		if err != nil {
			return 0, err
		}

		for _, dp := range dps {
			hd, ok := dp.(hdData)
			if !ok {
				continue
			}

			if hd.signatureType != hdSignatureGUID {
				continue
			}

			var uuidStr *C.char

			if rc := C.efi_guid_to_str((*C.efi_guid_t)(unsafe.Pointer(&hd.signature[0])), &uuidStr); rc < 0 {
				log.Errorf("Wrong PARTUUID in efi var: %s", getEfiError())
			}

			readUUID, err := uuid.Parse(C.GoString(uuidStr))
			if err != nil {
				log.Errorf("Wrong PARTUUID in efi var: %s", err)
				continue
			}

			if partUUID == readUUID {
				log.Debugf("Get EFI boot by PARTUUID=%s: %04X", partUUID, item.id)

				return item.id, nil
			}
		}
	}

	return 0, ErrNotFound
}

// GetBootCurrent returns boot current item
func (instance *Instance) GetBootCurrent() (id uint16, err error) {
	data, err := readU16(efiGlobalGUID, efiBootCurrentName)
	if err != nil {
		return 0, err
	}

	if len(data) != 1 {
		return 0, errors.New("invalid data size")
	}

	id = data[0]

	log.Debugf("Get EFI boot current: %04X", id)

	return id, nil
}

// GetBootNext returns boot next item
func (instance *Instance) GetBootNext() (id uint16, err error) {
	data, err := readU16(efiGlobalGUID, efiBootNextName)
	if err != nil {
		return 0, err
	}

	if len(data) != 1 {
		return 0, errors.New("invalid data size")
	}

	id = data[0]

	log.Debugf("Get EFI boot next: %04X", id)

	return id, nil
}

// SetBootNext sets boot next item
func (instance *Instance) SetBootNext(id uint16) (err error) {
	log.Debugf("Set EFI boot next: %04X", id)

	return writeU16(efiGlobalGUID, efiBootNextName, []uint16{id},
		C.EFI_VARIABLE_NON_VOLATILE|C.EFI_VARIABLE_BOOTSERVICE_ACCESS|C.EFI_VARIABLE_RUNTIME_ACCESS, writeAttribute)

}

// DeleteBootNext deletes boot next
func (instance *Instance) DeleteBootNext() (err error) {
	log.Debug("Delete EFI boot next")

	return deleteVar(efiGlobalGUID, efiBootNextName)
}

// GetBootOrder returns boot order
func (instance *Instance) GetBootOrder() (ids []uint16, err error) {
	if ids, err = readU16(efiGlobalGUID, efiBootOrderName); err != nil {
		return nil, err
	}

	log.Debugf("Get EFI boot order: %s", bootOrderToString(ids))

	return ids, nil
}

// SetBootOrder sets boot order
func (instance *Instance) SetBootOrder(ids []uint16) (err error) {
	log.Debugf("Set EFI boot order: %s", bootOrderToString(ids))

	return writeU16(efiGlobalGUID, efiBootOrderName, ids,
		C.EFI_VARIABLE_NON_VOLATILE|C.EFI_VARIABLE_BOOTSERVICE_ACCESS|C.EFI_VARIABLE_RUNTIME_ACCESS, writeAttribute)
}

// DeleteBootOrder deletes boot order
func (instance *Instance) DeleteBootOrder() (err error) {
	log.Debug("Delete EFI boot order")

	return deleteVar(efiGlobalGUID, efiBootOrderName)
}

// SetBootActive make boot item active
func (instance *Instance) SetBootActive(id uint16, active bool) (err error) {
	log.Debugf("Set EFI %04X boot active: %v", id, active)

	for i, item := range instance.bootItems {
		if item.id == id {
			cData := C.CBytes(item.data)
			efiLoadOption := (*C.efi_load_option)(cData)
			curActive := C.efi_loadopt_attrs(efiLoadOption)&loadOptionActive != 0

			if active == curActive {
				return nil
			}

			if active {
				C.efi_loadopt_attr_set(efiLoadOption, loadOptionActive)
			} else {
				C.efi_loadopt_attr_clear(efiLoadOption, loadOptionActive)
			}

			item.data = C.GoBytes(cData, C.int(len(item.data)))

			if err = writeVar(efiGlobalGUID, item.name, item.data, item.attributes, writeAttribute); err != nil {
				return err
			}

			instance.bootItems[i].data = item.data

			return nil
		}
	}

	return ErrNotFound
}

// GetBootActive returns boot item active state
func (instance *Instance) GetBootActive(id uint16) (active bool, err error) {
	for _, item := range instance.bootItems {
		if item.id == id {
			efiLoadOption := (*C.efi_load_option)(C.CBytes(item.data))
			active := C.efi_loadopt_attrs(efiLoadOption)&loadOptionActive != 0

			log.Debugf("Get EFI %04X boot active: %v", id, active)

			return active, nil
		}
	}

	return false, ErrNotFound
}

// Close closes EFI instance
func (instance *Instance) Close() (err error) {
	instance.bootItems = nil

	return nil
}

// CreateBootEntry creates new boot entry variable
func (instance *Instance) CreateBootEntry(isActive int, partitionPath string, loader string,
	entryName string) (id uint16, err error) {
	devicePath, err := partition.GetParentDevice(partitionPath)
	if err != nil {
		return 0, err
	}

	partition, err := partition.GetPartitionNum(partitionPath)
	if err != nil {
		return 0, err
	}

	item, err := instance.makeBootVar(isActive, devicePath, partition, loader, entryName)
	if err != nil {
		log.Errorf("Unable to create BootEntry variable: %s", err)
		return 0, err
	}

	ids, err := instance.GetBootOrder()
	if err != nil {
		log.Errorf("Unable to get efi boot order: %s", err)
		return 0, err
	}

	// Add to BootOrder
	ids = append(ids, item.id)

	if err = instance.SetBootOrder(ids); err != nil {
		log.Errorf("Unable to set efi boot order: %s", err)
		return 0, err
	}

	// Add to bootItems
	instance.bootItems = append(instance.bootItems, item)

	sort.Slice(instance.bootItems, func(i, j int) bool {
		return instance.bootItems[i].id < instance.bootItems[j].id
	})

	return item.id, nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func readVar(guid, name string) (data []byte, attributes uint32, err error) {
	var (
		efiData       *C.uint8_t
		efiSize       C.size_t
		efiAttributes C.uint32_t
		efiGUID       C.efi_guid_t
	)

	if rc := C.efi_str_to_guid(C.CString(guid), &efiGUID); rc < 0 {
		return nil, 0, getEfiError()
	}

	if rc := C.efi_get_variable(efiGUID, C.CString(name), &efiData, &efiSize, &efiAttributes); rc < 0 {
		return nil, 0, getEfiError()
	}

	return C.GoBytes(unsafe.Pointer(efiData), C.int(efiSize)), uint32(efiAttributes), nil
}

func readU16(guid, name string) (data []uint16, err error) {
	readData, _, err := readVar(guid, name)
	if err != nil {
		return nil, err
	}

	dataBuffer := bytes.NewBuffer(readData)

	data = make([]uint16, 0, 10)

	for {
		var id uint16

		if err = binary.Read(dataBuffer, binary.LittleEndian, &id); err != nil {
			if err != io.EOF {
				return nil, err
			}

			break
		}

		data = append(data, id)
	}

	return data, nil
}

func makeLinuxLoadOption(optionActive int, optionDisk string, optionPart int, optionLoader string,
	dataSize int, optionLabel string, optionalData []byte, optionalSize int) (data []byte, elemSize int, err error) {
	var attributes int32

	if optionActive != 0 {
		attributes = 1
	}

	options := efiBootAbbrevHD
	eddDeviceNum := eddDefaultDevice

	needed := C.efi_generate_file_device_path_from_esp_go(nil, 0,
		C.CString(optionDisk), C.int(optionPart), C.CString(optionLoader),
		C.uint32_t(options), C.uint32_t(eddDeviceNum))

	if needed < 0 {
		return nil, 0, fmt.Errorf("generate file device path failed")
	}

	dp := make([]byte, needed)

	if dataSize != 0 {
		if needed = C.efi_generate_file_device_path_from_esp_go((*C.uint8_t)(unsafe.Pointer(&dp[0])), C.ssize_t(needed),
			C.CString(optionDisk), C.int(optionPart), C.CString(optionLoader), C.uint32_t(options),
			C.uint32_t(eddDeviceNum)); needed < 0 {
			return nil, 0, fmt.Errorf("generate file device path failed")
		}

		// Allocating unsafeData
		data = make([]byte, dataSize)

		needed = C.efi_loadopt_create((*C.uint8_t)(unsafe.Pointer(&data[0])), C.ssize_t(dataSize), C.uint32_t(attributes),
			C.efidp(C.CBytes(dp)), C.ssize_t(needed), (*C.uchar)(unsafe.Pointer(C.CString(optionLabel))),
			(*C.uint8_t)(C.CBytes(optionalData)), C.size_t(optionalSize))
	} else {
		needed = C.efi_loadopt_create((*C.uint8_t)(unsafe.Pointer(nil)), C.ssize_t(0), C.uint32_t(attributes),
			C.efidp(C.CBytes(dp)), C.ssize_t(needed), (*C.uchar)(unsafe.Pointer(C.CString(optionLabel))),
			(*C.uint8_t)(C.CBytes(optionalData)), C.size_t(optionalSize))
	}

	if needed < 0 {
		return nil, 0, fmt.Errorf("efi load opt create failed")
	}

	return data, int(needed), nil
}

func (instance *Instance) findFreeNum() (num uint16, err error) {
	if len(instance.bootItems) == 0 {
		return 0, nil
	}

	num = instance.bootItems[0].id

	for _, v := range instance.bootItems {
		if num == v.id {
			num++
		} else if num < v.id {
			break
		}
	}

	return num, nil
}

func (instance *Instance) makeBootVar(isActive int, devicePath string, partition int, loader string,
	entryName string) (item bootItem, err error) {

	if item.id, err = instance.findFreeNum(); err != nil {
		return bootItem{}, err
	}

	_, needed, err := makeLinuxLoadOption(isActive, devicePath, partition, loader, 0, entryName, nil, 0)
	if needed < 0 || err != nil {
		return bootItem{}, err
	}

	var sz int

	item.data, sz, err = makeLinuxLoadOption(isActive, devicePath, partition, loader, needed, entryName, nil, 0)
	if sz < 0 || err != nil {
		return bootItem{}, err
	}

	item.name = fmt.Sprintf("Boot%04x", item.id)
	item.description = entryName
	item.attributes = C.EFI_VARIABLE_NON_VOLATILE | C.EFI_VARIABLE_BOOTSERVICE_ACCESS |
		C.EFI_VARIABLE_BOOTSERVICE_ACCESS | C.EFI_VARIABLE_RUNTIME_ACCESS

	if err := writeVar(efiGlobalGUID, item.name, item.data, item.attributes, writeAttribute); err != nil {
		return bootItem{}, err
	}

	return item, nil
}

func writeVar(guid, name string, data []byte, attributes uint32, mode os.FileMode) (err error) {
	var (
		efiGUID C.efi_guid_t
	)

	if rc := C.efi_str_to_guid(C.CString(guid), &efiGUID); rc < 0 {
		return getEfiError()
	}

	if rc := C.efi_set_variable(efiGUID, C.CString(name), (*C.uint8_t)(C.CBytes(data)),
		C.size_t(len(data)), C.uint32_t(attributes), C.mode_t(mode)); rc < 0 {
		return getEfiError()
	}

	return nil
}

func writeU16(guid, name string, data []uint16, attributes uint32, mode os.FileMode) (err error) {
	dataBuffer := &bytes.Buffer{}

	for _, value := range data {
		if err = binary.Write(dataBuffer, binary.LittleEndian, value); err != nil {
			return err
		}
	}

	if err = writeVar(guid, name, dataBuffer.Bytes(), attributes, mode); err != nil {
		return err
	}

	return nil
}

func deleteVar(guid, name string) (err error) {
	var (
		efiGUID C.efi_guid_t
	)

	if rc := C.efi_str_to_guid(C.CString(guid), &efiGUID); rc < 0 {
		return getEfiError()
	}

	if rc := C.efi_del_variable(efiGUID, C.CString(name)); rc < 0 {
		return getEfiError()
	}

	return nil
}

func getEfiError() (err error) {
	var (
		filename *C.char
		function *C.char
		line     C.int
		message  *C.char
		errCode  C.int
	)

	rc := C.efi_error_get(C.uint(0), &filename, &function, &line, &message, &errCode)
	if rc < 0 {
		return errors.New("can't get EFI error")
	}
	if rc == 0 {
		return errors.New("unknown error")
	}

	if syscall.Errno(errCode) == syscall.ENOENT {
		err = ErrNotFound
	} else {
		err = fmt.Errorf("%s: %s", C.GoString(message), syscall.Errno(errCode).Error())
	}

	C.efi_error_clear()

	return err
}

func bootOrderToString(bootOrder []uint16) (s string) {
	for _, order := range bootOrder {
		s = s + fmt.Sprintf("%04X,", order)
	}

	return strings.TrimSuffix(s, ",")
}

func (instance *Instance) readBootItems() (err error) {
	var guid *C.efi_guid_t = nil
	var name *C.char = nil

	for {
		if rc := C.efi_get_next_variable_name(&guid, &name); rc == 0 {
			break
		}

		n := C.GoString(name)

		if matched, _ := regexp.Match(bootItemNamePattern, []byte(n)); !matched {
			continue
		}

		var item bootItem

		if item, err = readBootItem(n); err != nil {
			log.Warnf("Skip boot item: %s", err)
			continue
		}

		instance.bootItems = append(instance.bootItems, item)
	}

	sort.Slice(instance.bootItems, func(i, j int) bool {
		return instance.bootItems[i].id < instance.bootItems[j].id
	})

	return nil
}

func readBootItem(name string) (item bootItem, err error) {
	item.name = name

	id, err := strconv.ParseUint(regexp.MustCompile(bootItemIDPattern).FindString(name), 16, 16)
	if err != nil {
		return bootItem{}, err
	}

	item.id = uint16(id)

	if item.data, item.attributes, err = readVar(efiGlobalGUID, name); err != nil {
		return bootItem{}, err
	}

	efiLoadOption := (*C.efi_load_option)(C.CBytes(item.data))
	item.description = C.GoString((*C.char)(unsafe.Pointer(C.efi_loadopt_desc(efiLoadOption, C.ssize_t(len(item.data))))))

	return item, nil
}

func parseDP(dpData []byte) (dps []interface{}, err error) {
	dps = make([]interface{}, 0)
	buffer := bytes.NewBuffer(dpData)

	for {
		var (
			dpType    uint8
			dpSubType uint8
			dpLen     uint16
		)

		if err = binary.Read(buffer, binary.LittleEndian, &dpType); err != nil {
			return nil, err
		}

		if err = binary.Read(buffer, binary.LittleEndian, &dpSubType); err != nil {
			return nil, err
		}

		if err = binary.Read(buffer, binary.LittleEndian, &dpLen); err != nil {
			return nil, err
		}

		if dpLen < 4 {
			return nil, errors.New("invalid dp size")
		}

		data := make([]byte, dpLen-4)

		if _, err = io.ReadFull(buffer, data); err != nil {
			return nil, err
		}

		switch dpType {
		case C.EFIDP_MEDIA_TYPE:
			dp, err := parseMediaType(dpSubType, data)
			if err != nil {
				return nil, err
			}

			dps = append(dps, dp)

		case C.EFIDP_END_TYPE:
			if dpSubType == C.EFIDP_END_ENTIRE {
				return dps, nil
			}
		}
	}
}

func parseMediaType(subType uint8, data []byte) (dp interface{}, err error) {
	switch subType {
	case C.EFIDP_MEDIA_HD:
		hd, err := parseHD(data)
		if err != nil {
			return nil, err
		}

		return hd, nil
	}

	return nil, nil
}

func parseHD(data []byte) (hd hdData, err error) {
	buffer := bytes.NewBuffer(data)

	if err = binary.Read(buffer, binary.LittleEndian, &hd.partNumber); err != nil {
		return hdData{}, err
	}

	if err = binary.Read(buffer, binary.LittleEndian, &hd.start); err != nil {
		return hdData{}, err
	}

	if err = binary.Read(buffer, binary.LittleEndian, &hd.size); err != nil {
		return hdData{}, err
	}

	if err = binary.Read(buffer, binary.LittleEndian, &hd.signature); err != nil {
		return hdData{}, err
	}

	if err = binary.Read(buffer, binary.LittleEndian, &hd.format); err != nil {
		return hdData{}, err
	}

	if err = binary.Read(buffer, binary.LittleEndian, &hd.signatureType); err != nil {
		return hdData{}, err
	}

	return hd, nil
}
