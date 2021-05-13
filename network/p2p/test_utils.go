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

func validators() []*collections.ValidatorShare {
	//pk := &bls.PublicKey{}
	//pk.Deserialize(refPk)
	return []*collections.ValidatorShare{
		{
			NodeID:      1,
			ValidatorPK: nil,
			ShareKey:    nil,
			Committee:   nil,
		},
	}
}

// TestValidatorStorage implementation
type TestValidatorStorage struct {
}

// LoadFromConfig implementation
func (v *TestValidatorStorage) LoadFromConfig(nodeID uint64, pubKey *bls.PublicKey, shareKey *bls.SecretKey, ibftCommittee map[uint64]*proto.Node) error {
	return nil
}
// SaveValidatorShare implementation
func (v *TestValidatorStorage) SaveValidatorShare(validator *collections.ValidatorShare) error {
	return nil
}
// GetAllValidatorsShare implementation
func (v *TestValidatorStorage) GetAllValidatorsShare() ([]*collections.ValidatorShare, error) {
	return validators(), nil
}
