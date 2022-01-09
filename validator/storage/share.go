package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
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
	NodeID       uint64
	PublicKey    *bls.PublicKey
	Committee    map[uint64]*proto.Node
	Metadata     *beacon.ValidatorMetadata // pointer in order to support nil
	OwnerAddress string
	operators    [][]byte
}

//  serializedShare struct
type serializedShare struct {
	NodeID       uint64
	ShareKey     []byte
	Committee    map[uint64]*proto.Node
	Metadata     *beacon.ValidatorMetadata // pointer in order to support nil
	OwnerAddress string
	Operators    [][]byte
}

// CommitteeSize returns the IBFT committee size
func (s *Share) CommitteeSize() int {
	return len(s.Committee)
}

// ThresholdSize returns the minimum IBFT committee members that needs to sign for a quorum (2F+1)
func (s *Share) ThresholdSize() int {
	return int(math.Ceil(float64(s.CommitteeSize()) * 2 / 3))
}

// PartialThresholdSize returns the minimum IBFT committee members that needs to sign for a partial quorum (F+1)
func (s *Share) PartialThresholdSize() int {
	return int(math.Ceil(float64(s.CommitteeSize()) * 1 / 3))
}

// OperatorPubKey returns the operator's public key based on the node id
func (s *Share) OperatorPubKey() (*bls.PublicKey, error) {
	if val, found := s.Committee[s.NodeID]; found {
		pk := &bls.PublicKey{}
		if err := pk.Deserialize(val.Pk); err != nil {
			return nil, errors.Wrap(err, "failed to deserialize public key")
		}
		return pk, nil
	}
	return nil, errors.New("could not find operator id in committee map")
}

// PubKeysByID returns the public keys with the associated ids
func (s *Share) PubKeysByID(ids []uint64) (PubKeys, error) {
	ret := make([]*bls.PublicKey, 0)
	for _, id := range ids {
		if val, ok := s.Committee[id]; ok {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(val.Pk); err != nil {
				return ret, errors.Wrap(err, "failed to deserialize public key")
			}
			ret = append(ret, pk)
		} else {
			return nil, errors.Errorf("pk for id (%d) not found", id)
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
		NodeID:       s.NodeID,
		Committee:    map[uint64]*proto.Node{},
		Metadata:     s.Metadata,
		OwnerAddress: s.OwnerAddress,
		Operators:    s.operators,
	}
	// copy committee by value
	for k, n := range s.Committee {
		value.Committee[k] = &proto.Node{
			IbftId: n.GetIbftId(),
			Pk:     n.GetPk()[:],
		}
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
		NodeID:       value.NodeID,
		PublicKey:    pubKey,
		Committee:    value.Committee,
		Metadata:     value.Metadata,
		OwnerAddress: value.OwnerAddress,
		operators:    value.Operators,
	}, nil
}

// HasMetadata returns true if the validator metadata was fetched
func (s *Share) HasMetadata() bool {
	return s.Metadata != nil
}

// OperatorReady returns true if all operator relevant data (node id, secret share, etc.) is present
func (s *Share) OperatorReady() bool {
	return s.NodeID != 0
}

// SetOperators set operators public keys
func (s *Share) SetOperators(ops [][]byte) {
	s.operators = make([][]byte, len(ops))
	copy(s.operators, ops[:])
	s.operators = ops
}

// HashOperators hash all operators keys key
func (s *Share) HashOperators() []string {
	hashes := make([]string, len(s.operators))
	for i, o := range s.operators {
		hashes[i] = fmt.Sprintf("%x", sha256.Sum256(o))
	}
	return hashes
}
