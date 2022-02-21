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

package tpmkey

import (
	"crypto"
	"encoding/asn1"
	"errors"
	"io"
	"math/big"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpmutil"

	"github.com/aoscloud/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Consts
 **********************************************************************************************************************/

/***********************************************************************************************************************
 * Types
 **********************************************************************************************************************/

// TPMKey TPM key instance.
type TPMKey interface {
	Password() (password string)
	MakePersistent(persistentHandle tpmutil.Handle) (err error)
}

type tpmKey struct {
	device           io.ReadWriter
	primaryHandle    tpmutil.Handle
	persistentHandle tpmutil.Handle
	publicKey        crypto.PublicKey
	privateBlob      []byte
	publicBlob       []byte
	password         string
}

type rsaKey struct {
	tpmKey
}

type eccKey struct {
	tpmKey
}

/***********************************************************************************************************************
 * Vars
 **********************************************************************************************************************/

var supportedHash = map[crypto.Hash]tpm2.Algorithm{ // nolint:gochecknoglobals
	crypto.SHA1:   tpm2.AlgSHA1,
	crypto.SHA256: tpm2.AlgSHA256,
	crypto.SHA384: tpm2.AlgSHA384,
	crypto.SHA512: tpm2.AlgSHA512,
}

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// CreateFromPersistent creates key from persistent storage.
// nolint:ireturn // we return different key types
func CreateFromPersistent(device io.ReadWriter, persistentHandle tpmutil.Handle) (key TPMKey, err error) {
	tpmPublic, _, _, err := tpm2.ReadPublic(device, persistentHandle)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	publicKey, err := tpmPublic.Key()
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return createNewKey(tpmPublic.Type, tpmKey{
		device:           device,
		persistentHandle: persistentHandle,
		publicKey:        publicKey,
	})
}

// CreateFromBlobs creates key from blobs.
// nolint:ireturn // we return different key types
func CreateFromBlobs(device io.ReadWriter, primaryHandle tpmutil.Handle,
	password string, privateBlob, publicBlob []byte) (key TPMKey, err error) {
	tpmPublic, err := tpm2.DecodePublic(publicBlob)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	publicKey, err := tpmPublic.Key()
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	return createNewKey(tpmPublic.Type, tpmKey{
		device:        device,
		primaryHandle: primaryHandle,
		password:      password,
		privateBlob:   privateBlob,
		publicBlob:    publicBlob,
		publicKey:     publicKey,
	})
}

/***********************************************************************************************************************
 * Private
 **********************************************************************************************************************/
//
// nolint:ireturn // we return different key types
func createNewKey(algorithm tpm2.Algorithm, tpmKey tpmKey) (key TPMKey, err error) {
	switch algorithm {
	case tpm2.AlgRSA:
		return &rsaKey{tpmKey: tpmKey}, nil

	case tpm2.AlgECC:
		return &eccKey{tpmKey: tpmKey}, nil

	default:
		return nil, aoserrors.New("unsupported key type")
	}
}

func makePersistent(key *tpmKey, persistentHandle tpmutil.Handle) (err error) {
	if key.persistentHandle != 0 {
		return aoserrors.New("key already in persistent storage")
	}

	keyHandle, _, err := tpm2.Load(key.device, key.primaryHandle, key.password, key.publicBlob, key.privateBlob)
	if err != nil {
		return aoserrors.Wrap(err)
	}

	defer func() {
		if flushErr := tpm2.FlushContext(key.device, keyHandle); flushErr != nil {
			if err == nil {
				err = aoserrors.Wrap(flushErr)
			}
		}
	}()

	// Clear slot
	if err = tpm2.EvictControl(
		key.device, key.password, tpm2.HandleOwner, persistentHandle, persistentHandle); err != nil {
		var tpmError tpm2.HandleError

		if !errors.As(err, &tpmError) {
			return aoserrors.Wrap(err)
		}

		if tpmError.Code != tpm2.RCHandle {
			return aoserrors.Wrap(err)
		}
	}

	if err = tpm2.EvictControl(key.device, key.password, tpm2.HandleOwner, keyHandle, persistentHandle); err != nil {
		return aoserrors.Wrap(err)
	}

	key.persistentHandle = persistentHandle

	return nil
}

func sign(key tpmKey, digest []byte, scheme tpm2.SigScheme) (signature []byte, err error) {
	var keyHandle tpmutil.Handle

	if key.persistentHandle == 0 {
		if keyHandle, _, err = tpm2.Load(key.device, key.primaryHandle, key.password,
			key.publicBlob, key.privateBlob); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		defer func() {
			if flushErr := tpm2.FlushContext(key.device, keyHandle); flushErr != nil {
				if err == nil {
					err = aoserrors.Wrap(flushErr)
				}
			}
		}()
	} else {
		keyHandle = key.persistentHandle
	}

	sig, err := tpm2.Sign(key.device, keyHandle, "", digest, nil, &scheme)
	if err != nil {
		return nil, aoserrors.Wrap(err)
	}

	switch sig.Alg {
	case tpm2.AlgRSASSA:
		return sig.RSA.Signature, nil

	case tpm2.AlgRSAPSS:
		return sig.RSA.Signature, nil

	case tpm2.AlgECDSA:
		sigStruct := struct{ R, S *big.Int }{sig.ECC.R, sig.ECC.S}

		signature, err = asn1.Marshal(sigStruct)

		return signature, aoserrors.Wrap(err)

	default:
		return nil, aoserrors.New("unsupported signing algorithm")
	}
}

func decryptRSA(key tpmKey, msg []byte, scheme tpm2.AsymScheme, label string) (plaintext []byte, err error) {
	var keyHandle tpmutil.Handle

	if key.persistentHandle == 0 {
		if keyHandle, _, err = tpm2.Load(key.device, key.primaryHandle, key.password,
			key.publicBlob, key.privateBlob); err != nil {
			return nil, aoserrors.Wrap(err)
		}

		defer func() {
			if flushErr := tpm2.FlushContext(key.device, keyHandle); flushErr != nil {
				if err == nil {
					err = aoserrors.Wrap(flushErr)
				}
			}
		}()
	} else {
		keyHandle = key.persistentHandle
	}

	plaintext, err = tpm2.RSADecrypt(key.device, keyHandle, "", msg, &scheme, label)

	return plaintext, aoserrors.Wrap(err)
}
