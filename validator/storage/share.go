package storage

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
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

// Share storage model
type Share struct {
	NodeID    uint64
	PublicKey *bls.PublicKey
	ShareKey  *bls.SecretKey
	Committee map[uint64]*proto.Node
}

//  serializedShare struct
type serializedShare struct {
	NodeID    uint64
	ShareKey  []byte
	Committee map[uint64]*proto.Node
}

// CommitteeSize returns the IBFT committee size
func (s *Share) CommitteeSize() int {
	return len(s.Committee)
}

// ThresholdSize returns the minimum IBFT committee members that needs to sign for a quorum
func (s *Share) ThresholdSize() int {
	return int(math.Ceil(float64(s.CommitteeSize()) * 2 / 3))
}

// PubKeysByID returns the public keys with the associated ids
func (s *Share) PubKeysByID(ids []uint64) (PubKeys, error) {
	ret := make([]*bls.PublicKey, 0)
	for _, id := range ids {
		if val, ok := s.Committee[id]; ok {
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
func (s *Share) VerifySignedMessage(msg *proto.SignedMessage) error {
	pks, err := s.PubKeysByID(msg.SignerIds)
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

// Serialize share to []byte
func (s *Share) Serialize() ([]byte, error) {
	value := serializedShare{
		NodeID:    s.NodeID,
		ShareKey:  s.ShareKey.Serialize(),
		Committee: s.Committee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode serializedValidator")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to Share model
func (s *Share) Deserialize(obj basedb.Obj) (*Share, error) {
	value := serializedShare{}
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&value); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	// in exporter scenario, share key should be nil
	if value.ShareKey != nil && len(value.ShareKey) > 0 {
		if err := shareSecret.Deserialize(value.ShareKey); err != nil {
			return nil, errors.Wrap(err, "Failed to get key secret")
		}
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(obj.Key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &Share{
		NodeID:    value.NodeID,
		PublicKey: pubKey,
		ShareKey:  shareSecret,
		Committee: value.Committee,
	}, nil
}

// ValidatorMsg represents a transferable object
type ValidatorMsg struct {
	NodeID    uint64
	ShareKey  []byte
	PublicKey  []byte
	Committee map[uint64]*proto.Node
}

// ToValidatorMessage returns a transferable object
func (s *Share) ToValidatorMessage() *ValidatorMsg {
	res := ValidatorMsg{
		NodeID:    s.NodeID,
		ShareKey:  s.ShareKey.Serialize(),
		Committee: s.Committee,
		PublicKey: s.PublicKey.Serialize(),
	}
	return &res
}
