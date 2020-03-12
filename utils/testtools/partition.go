package testtools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// This package contains different tools which are used in unit tests by
// different modules

/*******************************************************************************
 * Types
 ******************************************************************************/

// PartDesc partition description structure
type PartDesc struct {
	Type  string
	Label string
	Size  uint64
}

// PartInfo partition info structure
type PartInfo struct {
	PartDesc
	Device   string
	PartUUID uuid.UUID
}

// TestDisk test disk structure
type TestDisk struct {
	Device     string
	Partitions []PartInfo

	path string
}

/*******************************************************************************
 * Public
 ******************************************************************************/

// NewTestDisk creates new disk in file
func NewTestDisk(path string, desc []PartDesc) (disk *TestDisk, err error) {
	disk = &TestDisk{
		Partitions: make([]PartInfo, 0, len(desc)),
		path:       path}

	defer func(disk *TestDisk) {
		if err != nil {
			disk.Close()
		}
	}(disk)

	// skip 1M for GPT table etc. and add 1M after device
	var diskSize uint64 = 2

	for _, part := range desc {
		diskSize = diskSize + part.Size
	}

	var output []byte

	if output, err = exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M", "count="+strconv.FormatUint(diskSize, 10)).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	if output, err = exec.Command("parted", "-s", path, "mktable", "gpt").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	diskSize = 1

	for _, part := range desc {
		if output, err = exec.Command("parted", "-s", path, "mkpart", "primary",
			fmt.Sprintf("%dMiB", diskSize),
			fmt.Sprintf("%dMiB", diskSize+part.Size)).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%s (%s)", err, (string(output)))
		}

		diskSize = diskSize + part.Size
	}

	if output, err = exec.Command("losetup", "-f", "-P", path, "--show").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	disk.Device = strings.TrimSpace(string(output))

	for i, part := range desc {
		info := PartInfo{
			PartDesc: part,
			Device:   disk.Device + "p" + strconv.Itoa(i+1),
		}

		if info.PartUUID, err = getPartUUID(info.Device); err != nil {
			return nil, err
		}

		disk.Partitions = append(disk.Partitions, info)

		labelOption := "-L"

		if strings.Contains(part.Type, "fat") || strings.Contains(part.Type, "dos") {
			labelOption = "-n"
		}

		if output, err = exec.Command("mkfs."+part.Type, info.Device, labelOption, info.Label).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%s (%s)", err, (string(output)))
		}
	}

	return disk, nil
}

// Close closes test disk
func (disk *TestDisk) Close() (err error) {
	var output []byte

	if disk.Device != "" {
		if output, err = exec.Command("losetup", "-d", disk.Device).CombinedOutput(); err != nil {
			return fmt.Errorf("%s (%s)", err, (string(output)))
		}
	}

	if err = os.RemoveAll(disk.path); err != nil {
		return err
	}

	return nil
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func getPartUUID(device string) (partUUID uuid.UUID, err error) {
	var output []byte

	if output, err = exec.Command("blkid", device).CombinedOutput(); err != nil {
		return uuid.UUID{}, fmt.Errorf("%s (%s)", err, (string(output)))
	}

	for _, field := range strings.Fields(string(output)) {
		if strings.HasPrefix(field, "PARTUUID=") {
			if partUUID, err = uuid.Parse(strings.TrimPrefix(field, "PARTUUID=")); err != nil {
				return uuid.UUID{}, err
			}

			return partUUID, nil
		}
	}

	return uuid.UUID{}, errors.New("partition UUID not found")
}
