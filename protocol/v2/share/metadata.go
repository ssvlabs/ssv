package share

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

type Metadata struct {
	PublicKey    *bls.PublicKey
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	OperatorIDs  []uint64
	Liquidated   bool
}

type serializedMetadata struct {
	ShareKey     []byte
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	OperatorIDs  []uint64
	Liquidated   bool
}

// Serialize Metadata to []byte
func (s *Metadata) Serialize() ([]byte, error) {
	value := serializedMetadata{
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

// Deserialize key/value to Metadata model
func (s *Metadata) Deserialize(key []byte, val []byte) (*Metadata, error) {
	value := serializedMetadata{}
	d := gob.NewDecoder(bytes.NewReader(val))
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to get val value: %w", err)
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	// in exporter scenario, share key should be nil
	if value.ShareKey != nil && len(value.ShareKey) > 0 {
		if err := shareSecret.Deserialize(value.ShareKey); err != nil {
			return nil, fmt.Errorf("failed to get key secret: %w", err)
		}
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(key); err != nil {
		return nil, fmt.Errorf("failed to get pubkey: %w", err)
	}
	return &Metadata{
		PublicKey:    pubKey,
		Stats:        value.Stats,
		OwnerAddress: value.OwnerAddress,
		Operators:    value.Operators,
		OperatorIDs:  value.OperatorIDs,
		Liquidated:   value.Liquidated,
	}, nil
}

// BelongsToOperator checks whether the metadata belongs to operator
func (s *Metadata) BelongsToOperator(operatorPubKey string) bool {
	for _, pk := range s.Operators {
		if string(pk) == operatorPubKey {
			return true
		}
	}
	return false
}

// BelongsToOperatorID checks whether the metadata belongs to operator ID
func (s *Metadata) BelongsToOperatorID(operatorID uint64) bool {
	for _, id := range s.OperatorIDs {
		if id == operatorID {
			return true
		}
	}
	return false
}

func (s *Metadata) HasMetadata() bool {
	return s != nil
}

func (s *Metadata) HasStats() bool {
	return s.HasMetadata() && s.Stats != nil
}
