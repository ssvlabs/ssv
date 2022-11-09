package types

import (
	"bytes"
	"encoding/gob"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

// SSVShare is a combination of spectypes.Share and its ShareMetadata.
type SSVShare struct {
	spectypes.Share
	ShareMetadata
}

// Encode encodes SSVShare using gob.
func (s *SSVShare) Encode() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(s); err != nil {
		return nil, fmt.Errorf("failed to encode serializedValidator: %w", err)
	}

	return b.Bytes(), nil
}

// Decode decodes SSVShare using gob.
func (s *SSVShare) Decode(data []byte) error {
	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("failed to get val value: %w", err)
	}

	return nil
}

// BelongsToOperator checks whether the share belongs to operator.
func (s *SSVShare) BelongsToOperator(operatorPubKey string) bool {
	for _, pk := range s.Operators {
		if string(pk) == operatorPubKey {
			return true
		}
	}
	return false
}

// BelongsToOperatorID checks whether the share belongs to operator ID.
func (s *SSVShare) BelongsToOperatorID(operatorID spectypes.OperatorID) bool {
	for _, operator := range s.Committee {
		if operator.OperatorID == operatorID {
			return true
		}
	}
	return false
}

// HasStats checks whether the Stats field is not nil.
func (s *SSVShare) HasStats() bool {
	return s != nil && s.Stats != nil
}

// SetOperators sets operators public keys.
func (s *SSVShare) SetOperators(pks [][]byte) {
	s.Operators = make([][]byte, len(pks))
	copy(s.Operators, pks)
}

// ShareMetadata represents Metadata of SSVShare.
type ShareMetadata struct {
	Stats        *beaconprotocol.ValidatorMetadata
	OwnerAddress string
	Operators    [][]byte
	Liquidated   bool
}
