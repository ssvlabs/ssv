package migrations

import (
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate sszgen -path ./migration_6_model.go --objs migration_6_OldStorageShare

var oldSharesPrefix = []byte("shares_ssz/")

const addressLength = 20

type migration_6_OldStorageOperator struct {
	OperatorID uint64
	PubKey     []byte `ssz-max:"48"`
}

type migration_6_OldStorageShare struct {
	ValidatorIndex        uint64
	ValidatorPubKey       []byte                            `ssz-size:"48"`
	SharePubKey           []byte                            `ssz-max:"48"` // empty for not own shares
	Committee             []*migration_6_OldStorageOperator `ssz-max:"13"`
	Quorum, PartialQuorum uint64
	DomainType            [4]byte `ssz-size:"4"`
	FeeRecipientAddress   [addressLength]byte
	Graffiti              []byte `ssz-max:"32"`

	Status          uint64
	ActivationEpoch uint64
	OwnerAddress    [addressLength]byte
	Liquidated      bool
}

// Encode encodes Share using ssz.
func (s *migration_6_OldStorageShare) Encode() ([]byte, error) {
	result, err := s.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("marshal ssz: %w", err)
	}
	return result, nil
}

// Decode decodes Share using ssz.
func (s *migration_6_OldStorageShare) Decode(data []byte) error {
	if len(data) > types.MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), types.MaxAllowedShareSize)
	}
	if err := s.UnmarshalSSZ(data); err != nil {
		return fmt.Errorf("decode Share: %w", err)
	}
	s.Quorum, s.PartialQuorum = types.ComputeQuorumAndPartialQuorum(uint64(len(s.Committee)))
	return nil
}
