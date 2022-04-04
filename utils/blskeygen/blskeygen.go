package blskeygen

import "github.com/herumi/bls-eth-go-binary/bls"

// GenBLSKeyPair generated a BLS key pair.
func GenBLSKeyPair() (*bls.SecretKey, *bls.PublicKey) {
	_ = bls.Init(bls.BLS12_381)
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	return sk, sk.GetPublicKey()
}
