package partition_test

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/partition"
	"aos_updatemanager/utils/testtools"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var disk *testtools.TestDisk

var tmpDir string
var mountPoint string

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	if tmpDir, err = ioutil.TempDir("", "aos_test_"); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	mountPoint = path.Join(tmpDir, "mount")

	if disk, err = testtools.NewTestDisk(
		path.Join(tmpDir, "testdisk.img"),
		[]testtools.PartDesc{
			testtools.PartDesc{Type: "vfat", Label: "efi", Size: 16},
			testtools.PartDesc{Type: "ext4", Label: "platform", Size: 32},
		}); err != nil {
		log.Fatalf("Can't create test disk: %s", err)
	}

	ret := m.Run()

	if err = disk.Close(); err != nil {
		log.Fatalf("Can't close test disk: %s", err)
	}

	if err = os.RemoveAll(tmpDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestMountUmount(t *testing.T) {
	for _, part := range disk.Partitions {
		if err := partition.Mount(part.Device, mountPoint, part.Type); err != nil {
			t.Fatalf("Can't mount partition: %s", err)
		}

		if err := partition.Umount(mountPoint); err != nil {
			t.Fatalf("Can't umount partition: %s", err)
		}
	}
}

func TestMountAlreadyMounted(t *testing.T) {
	for _, part := range disk.Partitions {
		if err := partition.Mount(part.Device, mountPoint, part.Type); err != nil {
			t.Fatalf("Can't mount partition: %s", err)
		}

		if err := partition.Mount(part.Device, mountPoint, part.Type); err != nil {
			t.Fatalf("Can't mount partition: %s", err)
		}

		if err := partition.Umount(mountPoint); err != nil {
			t.Fatalf("Can't umount partition: %s", err)
		}
	}
}

func TestGetPartitionInfo(t *testing.T) {
	for _, part := range disk.Partitions {
		info, err := partition.GetInfo(part.Device)
		if err != nil {
			t.Fatalf("Can't get partition info: %s", err)
		}

		if part.Device != info.Device {
			t.Errorf("Wrong partition device: %s", info.Device)
		}

		if part.Type != info.Type {
			t.Errorf("Wrong partition type: %s", info.Type)
		}

		if part.Label != info.Label {
			t.Errorf("Wrong partition label: %s", info.Label)
		}

		if part.PartUUID != info.PartUUID {
			t.Errorf("Wrong partition UUID: %s", info.PartUUID)
		}
	}
}
