package types

import (
	"bytes"
	"encoding/gob"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

type SSVShare struct {
	spectypes.Share
	ShareMetadata
}

func (s *SSVShare) Encode() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(s); err != nil {
		return nil, fmt.Errorf("failed to encode serializedValidator: %w", err)
	}

	return b.Bytes(), nil
}

func (s *SSVShare) Decode(data []byte) error {
	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("failed to get val value: %w", err)
	}

	return nil
}

// BelongsToOperator checks whether the metadata belongs to operator
func (s *SSVShare) BelongsToOperator(operatorPubKey string) bool {
	for _, pk := range s.Operators {
		if string(pk) == operatorPubKey {
			return true
		}
	}
	return false
}

// BelongsToOperatorID checks whether the metadata belongs to operator ID
func (s *SSVShare) BelongsToOperatorID(operatorID uint64) bool {
	for _, id := range s.OperatorIDs {
		if id == operatorID {
			return true
		}
	}
	return false
}

func (s *SSVShare) HasStats() bool {
	return s != nil && s.Stats != nil
}

// SetOperators set Operators public keys
func (s *SSVShare) SetOperators(pks [][]byte) {
	s.Operators = make([][]byte, len(pks))
	copy(s.Operators, pks)
}

// SetOperatorIds set Operator ids
func (s *SSVShare) SetOperatorIds(ids []uint32) {
	s.OperatorIDs = make([]uint64, len(ids))
	for i, o := range ids {
		s.OperatorIDs[i] = uint64(o)
	}
}

type ShareMetadata struct {
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	OperatorIDs  []uint64
	Liquidated   bool
}
