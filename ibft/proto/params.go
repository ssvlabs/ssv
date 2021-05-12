package proto

import (
	"errors"
	"math"
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
		RoundChangeDuration:   int64(time.Second * 3),
		LeaderPreprepareDelay: int64(time.Second * 1),
	}
}

// CommitteeSize returns the IBFT committee size
func (p *InstanceParams) CommitteeSize() int {
	return len(p.IbftCommittee)
}

// ThresholdSize returns the minimum IBFT committee members that needs to sign for a quorum
func (p *InstanceParams) ThresholdSize() int {
	return int(math.Ceil(float64(len(p.IbftCommittee)) * 2 / 3))
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

// VerifySignedMessage returns true of signed message verifies against pks
func (p *InstanceParams) VerifySignedMessage(msg *SignedMessage) error {
	pks, err := p.PubKeysByID(msg.SignerIds)
	if err != nil {
		return err
	}
	if len(pks) == 0 {
		return errors.New("could not find public key")
	}

	res, err := msg.VerifyAggregatedSig(pks)
	if err != nil {
		return err
	}
	if !res {
		return errors.New("could not verify message signature")
	}

	return nil
}
