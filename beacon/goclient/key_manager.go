package goclient

import (
	"github.com/bloxapp/ssv/protocol/v1/message"

	"github.com/herumi/bls-eth-go-binary/bls"
)

func (gc *goClient) AddShare(shareKey *bls.SecretKey) error {
	return gc.keyManager.AddShare(shareKey)
}

func (gc *goClient) RemoveShare(pubKey string) error {
	return gc.keyManager.RemoveShare(pubKey)
}

func (gc *goClient) SignIBFTMessage(data message.Root, pk []byte, sigType message.SignatureType) ([]byte, error) {
	return gc.keyManager.SignIBFTMessage(data, pk, sigType)
}
