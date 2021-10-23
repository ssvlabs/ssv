package goclient

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

func (gc *goClient) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	root, err := message.SigningRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not get message signing root")
	}

	account, err := gc.wallet.AccountByPublicKey(hex.EncodeToString(pk))
	if err != nil {
		return nil, errors.Wrap(err, "could not get signing account")
	}

	sig, err := account.ValidationKeySign(root)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign message")
	}

	return sig, nil
}

func (gc *goClient) AddShare(shareKey *bls.SecretKey) error {
	_, err := gc.wallet.CreateValidatorAccountFromPrivateKey(shareKey.Serialize(), nil)
	return err
}
