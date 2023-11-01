//go:build !linux

package types

import "C"
import (
	"crypto"
)

func SignRSAPKCS1v15(priv []byte, h crypto.Hash, hashed []byte) ([]byte, error) {
	panic("implement")
}

func VerifyRSAPKCS1v15(pub []byte, h crypto.Hash, hashed, sig []byte) error {
	panic("implement")
}
