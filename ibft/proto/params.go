package proto

import (
	"errors"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// PubKeys defines the type for public keys object representation
type PubKeys []*bls.PublicKey

// Aggregate iterates over public keys and adds them to the bls PublicKey
func (keys PubKeys) Aggregate() bls.PublicKey {
	ret := bls.PublicKey{}
	for _, k := range keys {
		ret.Add(k)
	}
	return ret
}

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *ConsensusParams {
	return &ConsensusParams{
		RoundChangeDuration: int64(time.Second * 3),
	}
}

// CommitteeSize returns the IBFT committee size
func (p *InstanceParams) CommitteeSize() int {
	return len(p.IbftCommittee)
}

// PubKeysByID returns the public keys with the associated ids
func (p *InstanceParams) PubKeysByID(ids []uint64) (PubKeys, error) {
	ret := make([]*bls.PublicKey, 0)
	for _, id := range ids {
		if val, ok := p.IbftCommittee[id]; ok {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(val.Pk); err != nil {
				return ret, err
			}
			ret = append(ret, pk)
		} else {
			return nil, errors.New("pk for id not found")
		}
	}
	return ret, nil
}
