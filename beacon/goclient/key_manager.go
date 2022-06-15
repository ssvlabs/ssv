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

func (gc *goClient) SignIBFTMessage(message *message.ConsensusMessage, pk []byte, forkVersion string) ([]byte, error) {
	return gc.keyManager.SignIBFTMessage(message, pk, forkVersion)
}
