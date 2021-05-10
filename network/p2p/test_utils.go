package p2p

import (
	"github.com/bloxapp/ssv/fixtures"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var (
	refPk = fixtures.RefPk
)

func validators() []*collections.Validator {
	pk := &bls.PublicKey{}
	pk.Deserialize(refPk)
	return []*collections.Validator{
		{
			NodeID:      1,
			ValidatorPK: pk,
			ShareKey:    nil,
			Committee:   nil,
		},
	}
}

type TestValidatorStorage struct {
}

func (v *TestValidatorStorage) LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error {
	return nil
}

func (v *TestValidatorStorage) SaveValidatorShare(validator *collections.Validator) error {
	return nil
}

func (v *TestValidatorStorage) GetAllValidatorsShare() ([]*collections.Validator, error) {
	return validators(), nil
}
