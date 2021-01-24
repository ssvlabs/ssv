package types

import (
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
)

func DefaultConsensusParams() *ConsensusParams {
	return &ConsensusParams{
		RoundChangeDuration: int64(time.Second * 2),
	}
}

func (p *InstanceParams) CommitteeSize() int {
	return len(p.IbftCommittee)
}

// PubKeysById returns the public keys with the associated ids
func (p *InstanceParams) PubKeysById(ids []uint64) ([]bls.PublicKey, error) {
	ret := make([]bls.PublicKey, 0)
	for _, id := range ids {
		if val, ok := p.IbftCommittee[id]; ok {
			pk := bls.PublicKey{}
			if err := pk.Deserialize(val.Pk); err != nil {
				return ret, err
			}
			ret = append(ret, pk)
		}
	}
	return ret, nil
}
