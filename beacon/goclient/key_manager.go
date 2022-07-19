package goclient

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
)

func (gc *goClient) AddShare(shareKey *bls.SecretKey) error {
	return gc.keyManager.AddShare(shareKey)
}

func (gc *goClient) RemoveShare(pubKey string) error {
	return gc.keyManager.RemoveShare(pubKey)
}

func (gc *goClient) SignRoot(data spectypes.Root, signatureType spectypes.SignatureType, pk []byte) (spectypes.Signature, error) {
	return gc.keyManager.SignRoot(data, signatureType, pk)
}
