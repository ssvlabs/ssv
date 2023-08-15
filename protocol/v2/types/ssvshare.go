package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"sort"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

const (
	MaxPossibleShareSize = 1245
	MaxAllowedShareSize  = MaxPossibleShareSize * 8 // Leaving some room for protocol updates and calculation mistakes.
)

// SSVShare is a combination of spectypes.Share and its Metadata.
type SSVShare struct {
	spectypes.Share
	Metadata
}

// Encode encodes SSVShare using gob.
func (s *SSVShare) Encode() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(s); err != nil {
		return nil, fmt.Errorf("encode SSVShare: %w", err)
	}

	return b.Bytes(), nil
}

// Decode decodes SSVShare using gob.
func (s *SSVShare) Decode(data []byte) error {
	if len(data) > MaxAllowedShareSize {
		return fmt.Errorf("share size is too big, got %v, max allowed %v", len(data), MaxAllowedShareSize)
	}

	d := gob.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(s); err != nil {
		return fmt.Errorf("decode SSVShare: %w", err)
	}
	s.Quorum, s.PartialQuorum = ComputeQuorumAndPartialQuorum(len(s.Committee))
	return nil
}

// BelongsToOperator checks whether the share belongs to operator.
func (s *SSVShare) BelongsToOperator(operatorID spectypes.OperatorID) bool {
	return operatorID != 0 && s.OperatorID == operatorID
}

// HasBeaconMetadata checks whether the BeaconMetadata field is not nil.
func (s *SSVShare) HasBeaconMetadata() bool {
	return s != nil && s.BeaconMetadata != nil
}

func (s *SSVShare) SetFeeRecipient(feeRecipient bellatrix.ExecutionAddress) {
	s.FeeRecipientAddress = feeRecipient
}

// ComputeClusterIDHash will compute cluster ID hash with given owner address and operator ids
func ComputeClusterIDHash(ownerAddress []byte, operatorIds []uint64) ([]byte, error) {
	// Create a new hash
	hash := sha256.New()

	// Write the binary representation of the owner address to the hash
	hash.Write(ownerAddress)

	// Sort the array in ascending order
	sort.Slice(operatorIds, func(i, j int) bool {
		return operatorIds[i] < operatorIds[j]
	})

	// Write the values to the hash
	for _, id := range operatorIds {
		if err := binary.Write(hash, binary.BigEndian, id); err != nil {
			return nil, err
		}
	}

	return hash.Sum(nil), nil
}

func ComputeQuorumAndPartialQuorum(committeeSize int) (quorum uint64, partialQuorum uint64) {
	f := (committeeSize - 1) / 3
	return uint64(f*2 + 1), uint64(f + 1)
}

func ValidCommitteeSize(committeeSize int) bool {
	f := (committeeSize - 1) / 3
	return (committeeSize-1)%3 == 0 && f >= 1 && f <= 4
}

// Metadata represents metadata of SSVShare.
type Metadata struct {
	BeaconMetadata *beaconprotocol.ValidatorMetadata
	OwnerAddress   common.Address
	Liquidated     bool
}
