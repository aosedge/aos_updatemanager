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
	"crypto/rsa"
	"io"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpmutil"

	"github.com/aoscloud/aos_common/aoserrors"
)

/***********************************************************************************************************************
 * Public
 **********************************************************************************************************************/

// MakePersistent moves key to TPM persistent storage.
func (key *rsaKey) MakePersistent(persistentHandle tpmutil.Handle) (err error) {
	return aoserrors.Wrap(makePersistent(&key.tpmKey, persistentHandle))
}

// Public returns public key.
func (key *rsaKey) Public() (publicKey crypto.PublicKey) {
	return key.publicKey
}

// Password returns key password.
func (key *rsaKey) Password() (password string) {
	return key.password
}

// Sign signs digest with the private key.
func (key *rsaKey) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	alg := tpm2.AlgRSASSA

	if pssOpts, ok := opts.(*rsa.PSSOptions); ok {
		if pssOpts.SaltLength != rsa.PSSSaltLengthAuto {
			return nil, aoserrors.New("salt length must be rsa.PSSSaltLengthAuto")
		}

		alg = tpm2.AlgRSAPSS
	}

	tpmHash, ok := supportedHash[opts.HashFunc()]
	if !ok {
		return nil, aoserrors.Errorf("unsupported hash algorithm: %v", opts.HashFunc())
	}

	if len(digest) != opts.HashFunc().Size() {
		return nil, aoserrors.Errorf("wrong digest length: got %d, want %d", digest, opts.HashFunc().Size())
	}

	scheme := tpm2.SigScheme{
		Alg:  alg,
		Hash: tpmHash,
	}

	signature, err = sign(key.tpmKey, digest, scheme)

	return signature, aoserrors.Wrap(err)
}

// Decrypt decrypts data with private key.
func (key *rsaKey) Decrypt(rand io.Reader, msg []byte, opts crypto.DecrypterOpts) (plaintext []byte, err error) {
	var (
		scheme tpm2.AsymScheme
		label  string
	)

	switch opt := opts.(type) {
	case *rsa.OAEPOptions:
		hashOpt, ok := supportedHash[opt.Hash]
		if !ok {
			return nil, aoserrors.Errorf("unsupported hash algorithm: %v", opt.Hash)
		}

		scheme = tpm2.AsymScheme{
			Alg:  tpm2.AlgOAEP,
			Hash: hashOpt,
		}

		label = string(opt.Label)

	case nil, *rsa.PKCS1v15DecryptOptions:
		scheme = tpm2.AsymScheme{
			Alg: tpm2.AlgRSAES,
		}

	default:
		return nil, aoserrors.New("unsupported decrypt opts")
	}

	plaintext, err = decryptRSA(key.tpmKey, msg, scheme, label)

	return plaintext, aoserrors.Wrap(err)
}
