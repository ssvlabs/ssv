package types

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

type ShareMetadata struct {
	PublicKey    *bls.PublicKey
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	OperatorIDs  []uint64
	Liquidated   bool
}

type serializedShareMetadata struct {
	ShareKey     []byte
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	OperatorIDs  []uint64
	Liquidated   bool
}

// Serialize ShareMetadata to []byte
func (s *ShareMetadata) Serialize() ([]byte, error) {
	value := serializedShareMetadata{
		Stats:        s.Stats,
		OwnerAddress: s.OwnerAddress,
		Operators:    s.Operators,
		OperatorIDs:  s.OperatorIDs,
		Liquidated:   s.Liquidated,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, fmt.Errorf("failed to encode serializedValidator: %w", err)
	}

	return b.Bytes(), nil
}

// Deserialize key/value to ShareMetadata model
func (s *ShareMetadata) Deserialize(key []byte, val []byte) (*ShareMetadata, error) {
	value := serializedShareMetadata{}
	d := gob.NewDecoder(bytes.NewReader(val))
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to get val value: %w", err)
	}

	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(key); err != nil {
		return nil, fmt.Errorf("failed to get pubkey: %w", err)
	}

	return &ShareMetadata{
		PublicKey:    pubKey,
		Stats:        value.Stats,
		OwnerAddress: value.OwnerAddress,
		Operators:    value.Operators,
		OperatorIDs:  value.OperatorIDs,
		Liquidated:   value.Liquidated,
	}, nil
}

// BelongsToOperator checks whether the metadata belongs to operator
func (s *ShareMetadata) BelongsToOperator(operatorPubKey string) bool {
	for _, pk := range s.Operators {
		if string(pk) == operatorPubKey {
			return true
		}
	}
	return false
}

// BelongsToOperatorID checks whether the metadata belongs to operator ID
func (s *ShareMetadata) BelongsToOperatorID(operatorID uint64) bool {
	for _, id := range s.OperatorIDs {
		if id == operatorID {
			return true
		}
	}
	return false
}

func (s *ShareMetadata) HasStats() bool {
	return s != nil && s.Stats != nil
}
