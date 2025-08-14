//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package blskeygen

import "github.com/herumi/bls-eth-go-binary/bls"

// GenBLSKeyPair generated a BLS key pair.
func GenBLSKeyPair() (*bls.SecretKey, *bls.PublicKey) {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk, sk.GetPublicKey()
}
