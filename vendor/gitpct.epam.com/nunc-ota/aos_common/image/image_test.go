package image_test

import (
	"encoding/hex"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_common/image"
)

/*******************************************************************************
 * Consts
 ******************************************************************************/

const (
	workDir = "tmpFolder"
)

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

	if err = os.MkdirAll(workDir, 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	ret := m.Run()

	if err = os.RemoveAll(workDir); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

func TestUntarGZArchive(t *testing.T) {
	// test destination does not exist
	if err := image.UntarGZArchive(path.Join(workDir, "no_tar"), path.Join(workDir, "no_dest")); err == nil {
		t.Error("UntarGZArchive should failed:  destination does not exist")
	}

	if err := os.MkdirAll(path.Join(workDir, "outfolder"), 0755); err != nil {
		t.Fatalf("Error creating tmp dir %s", err)
	}

	if err := image.UntarGZArchive(path.Join(workDir, "no_tar"), path.Join(workDir, "outfolder")); err == nil {
		t.Error("UntarGZArchive should failed:  no such file or directory")
	}

	//test invalid archive
	if err := ioutil.WriteFile(path.Join(workDir, "testArchive.tar.gz"), []byte("This is test file"), 0644); err != nil {
		t.Fatalf("Can't write test file: %s", err)
	}

	if err := image.UntarGZArchive(path.Join(workDir, "testArchive.tar.gz"), ""); err == nil {
		t.Error("UntarGZArchive should failed: invalid header")
	}

	//prepare source folder and create archive
	if err := os.MkdirAll(path.Join(workDir, "archive_folder"), 0755); err != nil {
		t.Fatalf("Error creating tmp dir %s", err)
	}

	if err := os.MkdirAll(path.Join(workDir, "archive_folder", "dir1"), 0755); err != nil {
		t.Fatalf("Error creating tmp dir %s", err)
	}

	if err := ioutil.WriteFile(path.Join(workDir, "archive_folder", "file.txt"),
		[]byte("This is test file"), 0644); err != nil {
		t.Fatalf("Can't write test file: %s", err)
	}

	if err := ioutil.WriteFile(path.Join(workDir, "archive_folder", "dir1", "file2.txt"),
		[]byte("This is test file2"), 0644); err != nil {
		t.Fatalf("Can't write test file: %s", err)
	}

	command := exec.Command("tar", "-czf", path.Join(workDir, "test_archive.tar.gz"), "-C", path.Join(workDir, "archive_folder"), ".")
	if err := command.Run(); err != nil {
		t.Fatalf("Can't run tar: %s", err)
	}

	if err := image.UntarGZArchive(path.Join(workDir, "test_archive.tar.gz"), path.Join(workDir, "outfolder")); err != nil {
		t.Errorf("some issue with untar: %s", err)
	}

	//compare source dir and untarred dir
	command = exec.Command("git", "diff", "--no-index", path.Join(workDir, "archive_folder"), path.Join(workDir, "outfolder"))
	out, _ := command.Output()
	if string(out) != "" {
		t.Errorf("Untar content not identical")
	}
}

func TestDownload(t *testing.T) {
	handler, err := image.New()
	if err != nil {
		t.Fatalf("Create new handler fault: %s", err)
	}

	if _, err = handler.Download(workDir, "https://gobyexample.com/maps"); err != nil {
		t.Errorf("File can not be downloaded: %s", err)
	}

	if _, err = handler.Download(workDir, "fake_url"); err == nil {
		t.Errorf("Expect error becouse we use a fake URL: %s", err)
	}
}

func TestCreateFileInfo(t *testing.T) {
	fileNamePath := path.Join(workDir, "file")

	if err := ioutil.WriteFile(fileNamePath, []byte("Hello"), 0644); err != nil {
		t.Fatalf("Error create a new file: %s", err)
	}

	info, err := image.CreateFileInfo(fileNamePath)
	if err != nil {
		t.Errorf("Error creating file info: %s", err)
	}

	out, err := exec.Command("du", "-b", fileNamePath).Output()
	if err != nil {
		t.Fatalf("du returns error result: %s", err)
	}

	fileSize, err := strconv.ParseUint(strings.Fields(string(out))[0], 10, 64)
	if err != nil {
		t.Fatalf("Bad convertion str to int: %s", err)
	}
	if fileSize != info.Size {
		t.Errorf("Size of file mismatch. Expect: %d, actual: %d", fileSize, info.Size)
	}

	out, err = exec.Command("openssl", "dgst", "-sha3-256", fileNamePath).Output()
	if err != nil {
		t.Fatalf("openssl dgst -sha3-256 returns error result: %s", err)
	}

	shaStr := strings.Fields(string(out))
	actualCheckSum := hex.EncodeToString(info.Sha256)
	if shaStr[1] != actualCheckSum {
		t.Errorf("sha256 not equals. Expected: %s, actual: %s", shaStr[1], actualCheckSum)
	}

	out, err = exec.Command("openssl", "dgst", "-sha3-512", fileNamePath).Output()
	if err != nil {
		t.Fatalf("openssl dgst -sha3-512 returns error result: %s", err)
	}

	shaStr = strings.Fields(string(out))
	actualCheckSum = hex.EncodeToString(info.Sha512)
	if shaStr[1] != actualCheckSum {
		t.Errorf("sha512 not equals. Expected: %s, actual: %s", shaStr[1], actualCheckSum)
	}
}

func TestCheckFileInfo(t *testing.T) {
	fileNamePath := path.Join(workDir, "file")

	if err := ioutil.WriteFile(fileNamePath, []byte("Hello"), 0644); err != nil {
		t.Fatalf("Error create a new file: %s", fileNamePath)
	}

	info, err := image.CreateFileInfo(fileNamePath)
	if err != nil {
		t.Errorf("Can't create file info: %s", err)
	}

	if err = image.CheckFileInfo(fileNamePath, info); err != nil {
		t.Errorf("File info mismatch: %s", err)
	}

	// --- Negative cases
	// Bad file size case
	tmpFileSize := info.Size
	info.Size++
	if err = image.CheckFileInfo(fileNamePath, info); err == nil {
		t.Error("File size should not be matched")
	}

	info.Size = tmpFileSize

	// Bad sha256sum case
	tmpSha256 := info.Sha256[0]
	info.Sha256[0] -= 1
	if err = image.CheckFileInfo(fileNamePath, info); err == nil {
		t.Error("sha256 should not be matched")
	}

	info.Sha256[0] = tmpSha256

	// Bad sha512sum case
	info.Sha512[0] -= 1
	if err = image.CheckFileInfo(fileNamePath, info); err == nil {
		t.Error("sha512 should not be matched")
	}
}
