package goclient

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
)

func (gc *goClient) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	return nil, nil
}

func (gc *goClient) AddShare(shareKey *bls.SecretKey) error {
	return nil
}
