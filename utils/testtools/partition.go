package testtools

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// This package contains different tools which are used in unit tests by
// different modules

/*******************************************************************************
 * Public
 ******************************************************************************/

// MakeTestPartition makes file with ext4 and attach it to loop device
func MakeTestPartition(path, fsType string, size int) (device string, err error) {
	if err := exec.Command("dd", "if=/dev/zero", "of="+path, "bs=1M", "count="+strconv.Itoa(size)).Run(); err != nil {
		return "", err
	}

	if err := exec.Command("mkfs."+fsType, path).Run(); err != nil {
		return "", err
	}

	output, err := exec.Command("losetup", "-f", path, "--show").CombinedOutput()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// DeleteTestPartition detach partition file from loop device and delete the file
func DeleteTestPartition(device string) (err error) {
	output, err := exec.Command("losetup", "-l", "-O", "BACK-FILE", device).CombinedOutput()
	if err != nil {
		return err
	}

	path := strings.Split(string(output), "\n")[1]

	if err = exec.Command("losetup", "-d", device).Run(); err != nil {
		return err
	}

	if err = os.RemoveAll(path); err != nil {
		log.Fatalf("Can't remove partition file: %s", err)
	}

	return nil
}
