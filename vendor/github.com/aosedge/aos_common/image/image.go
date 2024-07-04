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

package image

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/opencontainers/go-digest"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/sha3"

	"github.com/aosedge/aos_common/aoserrors"
	"github.com/aosedge/aos_common/aostypes"
	"github.com/aosedge/aos_common/utils/contextreader"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

const (
	updateDownloadsTime = 10 * time.Second
)

const (
	manifestFileName = "manifest.json"
	blobsFolder      = "blobs"
	buffSize         = 1024 * 1024
)

const (
	dirSymlinkSize  = 4 * 1024
	contentTypeSize = 64
)

const (
	copyBufferSize     = 1024 * 1024
	copyBreathInterval = 5 * time.Second
	copyBreathTime     = 500 * time.Millisecond
)

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// FileInfo file info.
type FileInfo struct {
	Sha256 []byte
	Size   uint64
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// Download downloads the file by url.
func Download(ctx context.Context, destination, url string) (fileName string, err error) {
	log.WithField("url", url).Debug("Start downloading file")

	timer := time.NewTicker(updateDownloadsTime)
	defer timer.Stop()

	req, err := grab.NewRequest(destination, url)
	if err != nil {
		return "", aoserrors.Wrap(err)
	}

	req = req.WithContext(ctx)

	resp := grab.NewClient().Do(req)

	for {
		select {
		case <-timer.C:
			log.WithFields(log.Fields{
				"complete": resp.BytesComplete(), "total": resp.Size(),
			}).Debug("Download progress")

		case <-resp.Done:
			if err := resp.Err(); err != nil {
				if removeErr := os.RemoveAll(resp.Filename); removeErr != nil {
					log.Errorf("Can't remove download file: %v", removeErr)
				}

				return "", aoserrors.Wrap(err)
			}

			log.WithFields(log.Fields{"url": url, "file": resp.Filename}).Debug("Download complete")

			return resp.Filename, nil
		}
	}
}

// CheckFileInfo checks if file matches FileInfo.
func CheckFileInfo(ctx context.Context, fileName string, fileInfo FileInfo) (err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return aoserrors.Wrap(err)
	}

	if uint64(stat.Size()) != fileInfo.Size {
		return aoserrors.New("file size mismatch")
	}

	hash256 := sha3.New256()

	contextRead := contextreader.New(ctx, file)

	if _, err := io.Copy(hash256, contextRead); err != nil {
		return aoserrors.Wrap(err)
	}

	if !reflect.DeepEqual(hash256.Sum(nil), fileInfo.Sha256) {
		return aoserrors.New("checksum sha256 mismatch")
	}

	return nil
}

// CreateFileInfo creates FileInfo from existing file.
func CreateFileInfo(ctx context.Context, fileName string) (fileInfo FileInfo, err error) {
	file, err := os.Open(fileName)
	if err != nil {
		return fileInfo, aoserrors.Wrap(err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fileInfo, aoserrors.Wrap(err)
	}

	fileInfo.Size = uint64(stat.Size())

	hash256 := sha3.New256()

	contextRead := contextreader.New(ctx, file)

	if _, err := io.Copy(hash256, contextRead); err != nil {
		return fileInfo, aoserrors.Wrap(err)
	}

	fileInfo.Sha256 = hash256.Sum(nil)

	return fileInfo, nil
}

// UntarGZArchive extract data from tar.gz archive.
func UntarGZArchive(ctx context.Context, source, destination string) (err error) {
	if _, err = os.Stat(destination); os.IsNotExist(err) {
		return aoserrors.Wrap(err)
	}

	archive, err := os.Open(source)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer archive.Close()

	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	contextReader := contextreader.New(ctx, tarReader)

	for {
		select {
		case <-ctx.Done():
			return aoserrors.Wrap(ctx.Err())

		default:
			header, err := tarReader.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}

			if err != nil {
				return aoserrors.Wrap(err)
			}

			if err = untarItem(destination, header, contextReader); err != nil {
				return aoserrors.Wrap(err)
			}
		}
	}
}

// UnpackTarImage extracts tar image.
func UnpackTarImage(source, destination string) error {
	log.WithFields(log.Fields{"name": source, "destination": destination}).Debug("Unpack tar image")

	if _, err := os.Stat(source); err != nil {
		return aoserrors.Wrap(err)
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return aoserrors.Wrap(err)
	}

	if output, err := exec.Command("tar", "xf", source, "-C", destination).CombinedOutput(); err != nil {
		log.Errorf("Failed to unpack archive: %s", string(output))

		return aoserrors.Wrap(err)
	}

	return nil
}

// GetImageManifest  gets image manifest data from file.
func GetImageManifest(imagePath string) (*aostypes.ServiceManifest, error) {
	manifestJSON, err := os.ReadFile(path.Join(imagePath, manifestFileName))
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	var manifest aostypes.ServiceManifest

	if err = json.Unmarshal(manifestJSON, &manifest); err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return &manifest, nil
}

// GetLayersFromManifest gets image layers.
func GetLayersFromManifest(manifest *aostypes.ServiceManifest) (layers []string) {
	for _, layer := range manifest.Layers[1:] {
		layers = append(layers, string(layer.Digest))
	}

	return layers
}

// ValidateDigest validates digest.
func ValidateDigest(installDir string, digest digest.Digest) (err error) {
	if err = digest.Validate(); err != nil {
		return aoserrors.Wrap(err)
	}

	file, err := os.Open(path.Join(installDir, blobsFolder, string(digest.Algorithm()), digest.Hex()))
	if err != nil {
		return aoserrors.Wrap(err)
	}
	defer file.Close()

	buffer := make([]byte, buffSize)
	verifier := digest.Verifier()

	for {
		count, readErr := file.Read(buffer)
		if readErr != nil && readErr != io.EOF {
			return aoserrors.Wrap(readErr)
		}

		if _, err := verifier.Write(buffer[:count]); err != nil {
			return aoserrors.Wrap(err)
		}

		if readErr != nil {
			break
		}
	}

	if !verifier.Verified() {
		return aoserrors.New("hash missmach")
	}

	return nil
}

// GetUncompressedTarContentSize calculates tar content size.
func GetUncompressedTarContentSize(path string) (size int64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer file.Close()

	bReader := bufio.NewReader(file)

	testBytes, err := bReader.Peek(contentTypeSize)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}

	reader := io.Reader(bReader)

	if strings.Contains(http.DetectContentType(testBytes), "x-gzip") {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return 0, aoserrors.Wrap(err)
		}
		defer gzipReader.Close()

		reader = gzipReader
	}

	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return size, nil
		}

		if err != nil {
			return 0, aoserrors.Wrap(err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			size += dirSymlinkSize

		case tar.TypeSymlink:
			size += dirSymlinkSize

		case tar.TypeReg:
			size += header.Size
		}
	}
}

// Copy copies one file content to another.
func Copy(dst, src string) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Start copy")

	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	copied, duration, err := copyData(dstFile, srcFile)
	if err != nil {
		return copied, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"copied": copied, "duration": duration}).Debug("Copy finished")

	return copied, nil
}

// CopyFromGzipArchive copies gzip archive to file.
func CopyFromGzipArchive(dst, src string) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Start copy from gzip archive")

	srcFile, err := os.Open(src)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer gz.Close()

	copied, duration, err := copyData(dstFile, gz)
	if err != nil {
		return copied, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"copied": copied, "duration": duration}).Debug("Copy from gzip archive finished")

	return copied, nil
}

// CopyToDevice copies file content to device.
func CopyToDevice(dst, src string, direct bool) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Start copy to device")

	srcFile, err := os.Open(src)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	flags := os.O_RDWR | os.O_TRUNC
	if direct {
		flags |= syscall.O_DIRECT
	}

	dstFile, err := os.OpenFile(dst, flags, 0)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	copied, duration, err := copyData(dstFile, srcFile)
	if err != nil {
		return copied, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{"copied": copied, "duration": duration}).Debug("Copy to device finished")

	return copied, nil
}

// CopyFromGzipArchiveToDevice copies gzip archive to device.
func CopyFromGzipArchiveToDevice(dst, src string, direct bool) (copied int64, err error) {
	log.WithFields(log.Fields{"src": src, "dst": dst}).Debug("Start copy from gzip archive to device")

	srcFile, err := os.Open(src)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer srcFile.Close()

	flags := os.O_RDWR | os.O_TRUNC
	if direct {
		flags |= syscall.O_DIRECT
	}

	dstFile, err := os.OpenFile(dst, flags, 0)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer dstFile.Close()

	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return 0, aoserrors.Wrap(err)
	}
	defer gz.Close()

	copied, duration, err := copyData(dstFile, gz)
	if err != nil {
		return copied, aoserrors.Wrap(err)
	}

	log.WithFields(log.Fields{
		"copied": copied, "duration": duration,
	}).Debug("Copy from gzip archive to device finished")

	return copied, nil
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/

func untarItem(destination string, header *tar.Header, reader io.Reader) (err error) {
	itemPath, err := getItemPath(destination, header.Name)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if header.Name == "./" {
			break
		}

		if err := os.Mkdir(itemPath, os.FileMode(header.Mode)); err != nil {
			return aoserrors.Wrap(err)
		}

	case tar.TypeReg:
		outFile, err := os.Create(itemPath)
		if err != nil {
			return aoserrors.Wrap(err)
		}
		defer outFile.Close()

		if _, err := io.Copy(outFile, reader); err != nil {
			return aoserrors.Wrap(err)
		}

	default:
		log.Warning("Unknown tar Header type: ", header.Typeflag, header.Name)
	}

	return nil
}

func getItemPath(destination, name string) (itemPath string, err error) {
	itemPath = path.Join(destination, name)

	if !strings.HasPrefix(itemPath+string(os.PathSeparator), path.Clean(itemPath)) {
		return "", aoserrors.Errorf("illegal item path: %s", name)
	}

	return itemPath, nil
}

func copyData(dst io.Writer, src io.Reader) (copied int64, duration time.Duration, err error) {
	startTime := time.Now()
	buf := make([]byte, copyBufferSize)

	for !errors.Is(err, io.EOF) {
		var readCount int

		if readCount, err = src.Read(buf); err != nil && !errors.Is(err, io.EOF) {
			return copied, duration, aoserrors.Wrap(err)
		}

		if readCount > 0 {
			var writeCount int

			if writeCount, err = dst.Write(buf[:readCount]); err != nil {
				return copied, duration, aoserrors.Wrap(err)
			}

			copied += int64(writeCount)
		}

		if time.Now().After(startTime.Add(duration).Add(copyBreathInterval)) {
			time.Sleep(copyBreathTime)

			duration = time.Since(startTime)

			log.WithFields(log.Fields{"copied": copied, "duration": duration}).Debug("Copy progress")
		}
	}

	duration = time.Since(startTime)

	return copied, duration, nil
}
