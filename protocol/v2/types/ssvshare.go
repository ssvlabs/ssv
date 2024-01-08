package types

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
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
	var encodedData []byte

	// Sort the slice of uint64 in ascending order
	sort.Slice(operatorIds, func(i, j int) bool {
		return operatorIds[i] < operatorIds[j]
	})

	// Append bytes of the owner address
	encodedData = append(encodedData, ownerAddress...)

	// Append the ABI-encoded uint64 values
	for _, id := range operatorIds {
		encodedData = append(encodedData, abiEncodeUint64(id)...)
	}

	// Compute the keccak256 hash of the encoded data
	encrypted := crypto.Keccak256(encodedData)

	return encrypted, nil
}

// abiEncodeUint64 encodes a uint64 value for ABI encoding.
func abiEncodeUint64(value uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, value)
	return buf
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
