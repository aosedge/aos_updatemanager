package partition_test

import (
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/partition"
	"aos_updatemanager/utils/testtools"
)

/*******************************************************************************
 * Vars
 ******************************************************************************/

var device string

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

	if err = os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	if device, err = testtools.MakeTestPartition("tmp/partition", 8); err != nil {
		log.Fatalf("Can't create test partition: %s", err)
	}

	ret := m.Run()

	if err = testtools.DeleteTestPartition(device); err != nil {
		log.Fatalf("Can't delete test partition: %s", err)
	}

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestMountUmount(t *testing.T) {
	if err := partition.Mount(device, "tmp/mount", "ext4"); err != nil {
		t.Fatalf("Can't mount partition: %s", err)
	}

	if err := partition.Umount("tmp/mount"); err != nil {
		t.Fatalf("Can't umount partition: %s", err)
	}
}

func TestMountAlreadyMounted(t *testing.T) {
	if err := os.MkdirAll("tmp/mount", 0755); err != nil {
		log.Fatalf("Error mount dir: %s", err)
	}

	if err := partition.Mount(device, "tmp/mount", "ext4"); err != nil {
		t.Fatalf("Can't mount partition: %s", err)
	}

	if err := partition.Mount(device, "tmp/mount", "ext4"); err != nil {
		t.Fatalf("Can't mount partition: %s", err)
	}

	if err := partition.Umount("tmp/mount"); err != nil {
		t.Fatalf("Can't umount partition: %s", err)
	}
}
