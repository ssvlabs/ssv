package collections

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"math"
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

// ValidatorShare model for ValidatorStorage struct creation
type ValidatorShare struct {
	NodeID      uint64
	ValidatorPK *bls.PublicKey
	ShareKey    *bls.SecretKey
	Committee   map[uint64]*proto.Node
}

// CommitteeSize returns the IBFT committee size
func (v *ValidatorShare) CommitteeSize() int {
	return len(v.Committee)
}

// ThresholdSize returns the minimum IBFT committee members that needs to sign for a quorum
func (v *ValidatorShare) ThresholdSize() int {
	return int(math.Ceil(float64(v.CommitteeSize()) * 2 / 3))
}

// PubKeysByID returns the public keys with the associated ids
func (v *ValidatorShare) PubKeysByID(ids []uint64) (PubKeys, error) {
	ret := make([]*bls.PublicKey, 0)
	for _, id := range ids {
		if val, ok := v.Committee[id]; ok {
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
func (v *ValidatorShare) VerifySignedMessage(msg *proto.SignedMessage) error {
	pks, err := v.PubKeysByID(msg.SignerIds)
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

// Serialize ValidatorStorage to []byte for db purposes
func (v *ValidatorShare) Serialize() ([]byte, error) {
	value := validatorSerializer{
		NodeID:     v.NodeID,
		ShareKey:   v.ShareKey.Serialize(),
		Committiee: v.Committee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode validatorSerializer")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to ValidatorStorage struct
func (v *ValidatorShare) Deserialize(obj storage.Obj) (*ValidatorShare, error) {
	var valShare validatorSerializer
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&valShare); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	if err := shareSecret.Deserialize(valShare.ShareKey); err != nil {
		return nil, errors.Wrap(err, "Failed to get key secret")
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(obj.Key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &ValidatorShare{
		NodeID:      valShare.NodeID,
		ValidatorPK: pubKey,
		ShareKey:    shareSecret,
		Committee:   valShare.Committiee,
	}, nil
}
