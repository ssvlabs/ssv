package types

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"sort"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

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

func (s *SSVShare) IsAttesting(epoch phase0.Epoch) bool {
	return s.HasBeaconMetadata() &&
		(s.BeaconMetadata.IsAttesting() || (s.BeaconMetadata.Status == eth2apiv1.ValidatorStatePendingQueued && s.BeaconMetadata.ActivationEpoch <= epoch))
}

func (s *SSVShare) SetFeeRecipient(feeRecipient bellatrix.ExecutionAddress) {
	s.FeeRecipientAddress = feeRecipient
}

// ComputeClusterIDHash will compute cluster ID hash with given owner address and operator ids
func ComputeClusterIDHash(address common.Address, operatorIds []uint64) []byte {
	sort.Slice(operatorIds, func(i, j int) bool {
		return operatorIds[i] < operatorIds[j]
	})

	// Encode the address and operator IDs in the same way as Solidity's abi.encodePacked
	var data []byte
	data = append(data, address.Bytes()...) // Address is 20 bytes
	for _, id := range operatorIds {
		idBytes := make([]byte, 32)                  // Each ID should be 32 bytes
		binary.BigEndian.PutUint64(idBytes[24:], id) // PutUint64 fills the last 8 bytes; rest are 0
		data = append(data, idBytes...)
	}

	// Hash the data using keccak256
	hash := crypto.Keccak256(data)
	return hash
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

	// lastUpdated is an internal field that can be used to track the last time the metadata was updated.
	lastUpdated time.Time
}

func (m *Metadata) MetadataLastUpdated() time.Time {
	return m.lastUpdated
}

func (m *Metadata) SetMetadataLastUpdated(t time.Time) {
	m.lastUpdated = t
}
