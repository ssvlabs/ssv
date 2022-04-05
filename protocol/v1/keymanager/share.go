package keymanager

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
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

// Node represent committee member info
type Node struct {
	IbftId uint64
	Pk     []byte
}

// Share storage model
type Share struct {
	NodeID       OperatorID
	PublicKey    *bls.PublicKey
	Committee    map[OperatorID]*Node
	Metadata     *beacon.ValidatorMetadata // pointer in order to support nil
	OwnerAddress string
	Operators    [][]byte
}

//  serializedShare struct
type serializedShare struct {
	NodeID       OperatorID
	ShareKey     []byte
	Committee    map[OperatorID]*Node
	Metadata     *beacon.ValidatorMetadata // pointer in order to support nil
	OwnerAddress string
	Operators    [][]byte
}

// IsOperatorShare checks whether the share belongs to operator
func (s *Share) IsOperatorShare(operatorPubKey string) bool {
	for _, op := range s.Operators {
		if string(op) == operatorPubKey {
			return true
		}
	}
	return false
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
func (s *Share) PubKeysByID(ids []OperatorID) (PubKeys, error) {
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
func (s *Share) VerifySignedMessage(msg *message.SignedMessage) error {
	pks, err := s.PubKeysByID(msg.GetSigners())
	if err != nil {
		return err
	}
	if len(pks) == 0 {
		return errors.New("could not find public key")
	}

	var operators []*Operator
	for _, id := range msg.GetSigners() {
		operators = append(operators, &Operator{OperatorID: id})
	}
	err = msg.GetSignature().VerifyByOperators(msg, PrimusTestnet, QBFTSigType, operators) // TODO need to check if this is the right verify func
	//res, err := msg.VerifyAggregatedSig(pks)
	if err != nil {
		return err
	}
	//if !res {
	//	return errors.New("could not verify message signature")
	//}

	return nil
}

// Serialize share to []byte
func (s *Share) Serialize() ([]byte, error) {
	value := serializedShare{
		NodeID:       s.NodeID,
		Committee:    map[OperatorID]*Node{},
		Metadata:     s.Metadata,
		OwnerAddress: s.OwnerAddress,
		Operators:    s.Operators,
	}
	// copy committee by value
	for k, n := range s.Committee {
		value.Committee[k] = &Node{
			IbftId: n.IbftId,
			Pk:     n.Pk[:],
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
func (s *Share) Deserialize(key []byte, val []byte) (*Share, error) {
	value := serializedShare{}
	d := gob.NewDecoder(bytes.NewReader(val))
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
	if err := pubKey.Deserialize(key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &Share{
		NodeID:       value.NodeID,
		PublicKey:    pubKey,
		Committee:    value.Committee,
		Metadata:     value.Metadata,
		OwnerAddress: value.OwnerAddress,
		Operators:    value.Operators,
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

// SetOperators set Operators public keys
func (s *Share) SetOperators(ops [][]byte) {
	s.Operators = make([][]byte, len(ops))
	copy(s.Operators, ops[:])
	s.Operators = ops
}

// HashOperators hash all Operators keys key
func (s *Share) HashOperators() []string {
	hashes := make([]string, len(s.Operators))
	for i, o := range s.Operators {
		hashes[i] = fmt.Sprintf("%x", sha256.Sum256(o))
	}
	return hashes
}
