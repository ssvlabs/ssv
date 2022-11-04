package share

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"math"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/types"
)

// Share storage model
//type Share struct {
//	NodeID       spectypes.OperatorID
//	PublicKey    *bls.PublicKey
//	Committee    map[spectypes.OperatorID]*Node
//	Metadata     *ValidatorMetadata // pointer in order to support nil
//	OwnerAddress string
//	Operators    [][]byte
//	OperatorIds  []uint64
//	Liquidated   bool
//}

// Share storage model
type Share struct {
	NodeID      spectypes.OperatorID
	PublicKey   *bls.PublicKey
	Committee   map[spectypes.OperatorID]*beaconprotocol.Node
	OperatorIDs []uint64 // TODO: may be removed and taken from Committee
}

type SpecShare = spectypes.Share // not needed; used for quick lookup

type serializedShare struct {
	NodeID      spectypes.OperatorID
	ShareKey    []byte
	Committee   map[spectypes.OperatorID]*beaconprotocol.Node
	OperatorIDs []uint64
}

// IsOperatorShare checks whether the share belongs to operator
func (s *Share) IsOperatorShare(operatorID uint64) bool {
	for _, id := range s.OperatorIDs {
		if id == operatorID {
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

// HasPartialQuorum returns true if at least f+1 items present (cnt is the number of items). It assumes nothing about those items, not their type or structure.
// https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/main/dafny/spec/L1/node_auxiliary_functions.dfy#L244
func (s *Share) HasPartialQuorum(cnt int) bool {
	return cnt >= s.PartialThresholdSize()
}

// HasQuorum returns true if at least 2f+1 items are present (cnt is the number of items). It assumes nothing about those items, not their type or structure
// https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/main/dafny/spec/L1/node_auxiliary_functions.dfy#L259
func (s *Share) HasQuorum(cnt int) bool {
	return cnt >= s.ThresholdSize()
}

// OperatorSharePubKey returns the operator's public key based on the node id
func (s *Share) OperatorSharePubKey() (*bls.PublicKey, error) {
	if val, found := s.Committee[s.NodeID]; found {
		pk := &bls.PublicKey{}
		if err := pk.Deserialize(val.Pk); err != nil {
			return nil, fmt.Errorf("failed to deserialize public key: %w", err)
		}
		return pk, nil
	}
	return nil, errors.New("could not find operator id in committee map")
}

// PubKeysByID returns the public keys with the associated ids
func (s *Share) PubKeysByID(ids []spectypes.OperatorID) (map[spectypes.OperatorID]*bls.PublicKey, error) {
	//ret := make([]*bls.PublicKey, 0)
	ret := make(map[spectypes.OperatorID]*bls.PublicKey)
	for _, id := range ids {
		if val, ok := s.Committee[id]; ok {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(val.Pk); err != nil {
				return ret, fmt.Errorf("failed to deserialize public key: %w", err)
			}
			ret[id] = pk
		} else {
			return nil, fmt.Errorf("pk for id (%d) not found", id)
		}
	}
	return ret, nil
}

// VerifySignedMessage returns true of signed message verifies against pks
func (s *Share) VerifySignedMessage(msg *specqbft.SignedMessage) error {
	pks, err := s.PubKeysByID(msg.GetSigners())
	if err != nil {
		return err
	}
	if len(pks) == 0 {
		return errors.New("could not find public key")
	}

	var operators []*spectypes.Operator
	for _, id := range msg.GetSigners() {
		pk := pks[id]
		operators = append(operators, &spectypes.Operator{
			OperatorID: id,
			PubKey:     pk.Serialize(),
		})
	}

	err = msg.GetSignature().VerifyByOperators(msg, types.GetDefaultDomain(), spectypes.QBFTSignatureType, operators)
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
		NodeID:      s.NodeID,
		Committee:   map[spectypes.OperatorID]*beaconprotocol.Node{},
		OperatorIDs: s.OperatorIDs,
	}
	// copy committee by value
	for k, n := range s.Committee {
		value.Committee[k] = &beaconprotocol.Node{
			IbftID: n.IbftID,
			Pk:     n.Pk[:],
		}
	}
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, fmt.Errorf("failed to encode serializedValidator: %w", err)
	}
	return b.Bytes(), nil
}

// Deserialize key/value to Share model
func (s *Share) Deserialize(key []byte, val []byte) (*Share, error) {
	value := serializedShare{}
	d := gob.NewDecoder(bytes.NewReader(val))
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to get val value: %w", err)
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	// in exporter scenario, share key should be nil
	if value.ShareKey != nil && len(value.ShareKey) > 0 {
		if err := shareSecret.Deserialize(value.ShareKey); err != nil {
			return nil, fmt.Errorf("Failed to get key secret: %w", err)
		}
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(key); err != nil {
		return nil, fmt.Errorf("failed to get pubkey: %w", err)
	}
	return &Share{
		NodeID:      value.NodeID,
		PublicKey:   pubKey,
		Committee:   value.Committee,
		OperatorIDs: value.OperatorIDs,
	}, nil
}

// OperatorReady returns true if all operator relevant data (node id, secret share, etc.) is present
func (s *Share) OperatorReady() bool {
	return s.NodeID != 0
}

// SetOperators set Operators public keys
func (s *Share) SetOperators(opIDs []uint32) {
	s.OperatorIDs = make([]uint64, len(opIDs))
	for i, o := range opIDs {
		s.OperatorIDs[i] = uint64(o)
	}
}

// HashOperators hash all Operators keys key
func (s *Share) HashOperators() []string {
	b := make([]byte, 4)
	hashes := make([]string, len(s.OperatorIDs))
	for i, o := range s.OperatorIDs {
		binary.LittleEndian.PutUint32(b, uint32(o))
		hashes[i] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	return hashes
}
