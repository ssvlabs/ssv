//go:build linux

package validation

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/cornelk/hashmap"
	"github.com/microsoft/go-crypto-openssl/openssl"
	"github.com/microsoft/go-crypto-openssl/openssl/bbig/bridge"
)

func init() {
	if err := openssl.Init(); err != nil {
		panic(err)
	}
}

// func BenchmarkVerifyPKCS1v15OpenSSL(b *testing.B) {
// 	dataOpenSSL := []byte("This is test data for OpenSSL verification.")
// 	hashedOpenSSL := sha256.Sum256(dataOpenSSL)

// 	priv, pub := newOpenSSLRSAKey(2048)

// 	sig, err := openssl.SignRSAPKCS1v15(priv, crypto.SHA256, hashedOpenSSL[:])
// 	if err != nil {
// 		b.Fatal(err)
// 	}

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		err := openssl.VerifyRSAPKCS1v15(pub, crypto.SHA256, hashedOpenSSL[:], sig)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// func BenchmarkSignPKCS1v15OpenSSL(b *testing.B) {
// 	dataOpenSSL := []byte("This is test data for OpenSSL verification.")
// 	hashedOpenSSL := sha256.Sum256(dataOpenSSL)

// 	priv, _ := newOpenSSLRSAKey(2048)

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		_, err := openssl.SignRSAPKCS1v15(priv, crypto.SHA256, hashedOpenSSL[:])
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// func newOpenSSLRSAKey(size int) (*openssl.PrivateKeyRSA, *openssl.PublicKeyRSA) {
// 	N, E, D, P, Q, Dp, Dq, Qinv, err := bridge.GenerateKeyRSA(size)
// 	if err != nil {
// 		panic(fmt.Sprintf("GenerateKeyRSA(%d): %v", size, err))
// 	}
// 	priv, err := bridge.NewPrivateKeyRSA(N, E, D, P, Q, Dp, Dq, Qinv)
// 	if err != nil {
// 		panic(fmt.Sprintf("NewPrivateKeyRSA(%d): %v", size, err))
// 	}
// 	pub, err := bridge.NewPublicKeyRSA(N, E)
// 	if err != nil {
// 		panic(fmt.Sprintf("NewPublicKeyRSA(%d): %v", size, err))
// 	}
// 	return priv, pub
// }

func verifyPKCS1v15(pub *openssl.PublicKeyRSA, message []byte, signature []byte) error {
	hashed := sha256.Sum256(message)
	err := openssl.VerifyRSAPKCS1v15(pub, crypto.SHA256, hashed[:], signature)
	if err != nil {
		return fmt.Errorf("verify RSA: %w", err)
	}
	return nil
}

var cache = hashmap.New[spectypes.OperatorID, *openssl.PublicKeyRSA]()

func (mv *messageValidator) verifyRSASignature(messageData []byte, operatorID spectypes.OperatorID, signature []byte) error {
	openSSLRSAPubKey, ok := cache.Get(operatorID)
	if !ok {
		operator, found, err := mv.nodeStorage.GetOperatorData(nil, operatorID)
		if err != nil {
			e := ErrOperatorNotFound
			e.got = operatorID
			e.innerErr = err
			return e
		}
		if !found {
			e := ErrOperatorNotFound
			e.got = operatorID
			return e
		}

		operatorPubKey, err := base64.StdEncoding.DecodeString(string(operator.PublicKey))
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("decode public key: %w", err)
			return e
		}

		rsaPubKey, err := rsaencryption.ConvertPemToPublicKey(operatorPubKey)
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("convert PEM: %w", err)
			return e
		}

		openSSLRSAPubKey, err = bridge.NewPublicKeyRSA(rsaPubKey.N, big.NewInt(int64(rsaPubKey.E)))
		if err != nil {
			e := ErrRSADecryption
			e.innerErr = fmt.Errorf("new public key: %w", err)
			return e
		}

		cache.Set(operatorID, openSSLRSAPubKey)
	}

	if err := verifyPKCS1v15(openSSLRSAPubKey, messageData, signature); err != nil {
		e := ErrRSADecryption
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", operatorID, err)
		return e
	}

	return nil
}
