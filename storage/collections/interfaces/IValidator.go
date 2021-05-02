package interfaces

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
)

type IValidator interface {
	LoadFromConfig(validatorPubKey *bls.PublicKey, sharePrivateKey *bls.SecretKey, committee map[uint64]*proto.Node)
	SaveValidatorShare(pubkey *bls.PublicKey, shareKey *bls.SecretKey, committiee map[uint64]*proto.Node) error
	GetValidatorShare() (*bls.PublicKey, *bls.SecretKey, map[uint64]*proto.Node, error)
}
