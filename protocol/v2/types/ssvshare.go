package types

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	MaxPossibleShareSize = 1326
	MaxAllowedShareSize  = MaxPossibleShareSize * 8 // Leaving some room for protocol updates and calculation mistakes.
)

// SSVShare is a spectypes.Share with extra data that fully describes SSV validator share.
type SSVShare struct {
	spectypes.Share

	// Status is validator (this share belongs to) state.
	Status eth2apiv1.ValidatorState
	// ActivationEpoch is validator (this share belongs to) epoch it activates at.
	ActivationEpoch phase0.Epoch
	// ExitEpoch is the epoch at which the validator (that this share belongs to) exited.
	ExitEpoch phase0.Epoch
	// OwnerAddress is validator (this share belongs to) owner address.
	OwnerAddress common.Address
	// Liquidated is validator (this share belongs to) liquidation status (true or false).
	Liquidated bool

	// BeaconMetadataLastUpdated is used to keep track of share last update time. Note, we don't
	// store this field in DB - it just serves as a helper-indicator for when we might want
	// to update SSVShare metadata we fetch from Beacon node so it doesn't get super stale.
	BeaconMetadataLastUpdated time.Time

	// committeeID is a cached value for committee ID so we don't recompute it every time.
	committeeID atomic.Pointer[spectypes.CommitteeID]

	// minParticipationEpoch is the epoch at which the validator can start participating.
	// This is set on registration and on every reactivation.
	//
	// TODO: this is not persistent yet, so we should assume zero values are already participating for now.
	minParticipationEpoch phase0.Epoch
}

// BelongsToOperator checks whether the share belongs to operator.
func (s *SSVShare) BelongsToOperator(operatorID spectypes.OperatorID) bool {
	if operatorID == 0 {
		return false
	}

	return slices.ContainsFunc(s.Committee, func(shareMember *spectypes.ShareMember) bool {
		return shareMember.Signer == operatorID
	})
}

// HasBeaconMetadata checks whether SSVShare has been enriched with respective Beacon metadata.
func (s *SSVShare) HasBeaconMetadata() bool {
	return s != nil && s.Status != eth2apiv1.ValidatorStateUnknown
}

func (s *SSVShare) IsAttesting(epoch phase0.Epoch) bool {
	return s.HasBeaconMetadata() &&
		(s.Status.IsAttesting() || (s.Status == eth2apiv1.ValidatorStatePendingQueued && s.ActivationEpoch <= epoch))
}

// Pending returns true if the validator is pending
func (s *SSVShare) Pending() bool {
	return s.Status.IsPending()
}

// Activated returns true if the validator is not unknown. It might be pending activation or active
func (s *SSVShare) Activated() bool {
	return s.Status.HasActivated() || s.Status.IsActive() || s.Status.IsAttesting()
}

// IsActive returns true if the validator is currently active. Cant be other state
func (s *SSVShare) IsActive() bool {
	return s.Status == eth2apiv1.ValidatorStateActiveOngoing
}

// Exiting returns true if the validator is exiting or exited
func (s *SSVShare) Exiting() bool {
	return s.Status.IsExited() || s.Status.HasExited()
}

// Slashed returns true if the validator is exiting or exited due to slashing
func (s *SSVShare) Slashed() bool {
	return s.Status == eth2apiv1.ValidatorStateExitedSlashed || s.Status == eth2apiv1.ValidatorStateActiveSlashed
}

// IsParticipating returns true if the validator can participate in *any* SSV duty at the given epoch.
// Note: the validator may be eligible only for sync committee, but not to attest and propose. See IsParticipatingAndAttesting.
// Requirements: not liquidated and attesting or exited in the current or previous sync committee period.
func (s *SSVShare) IsParticipating(cfg networkconfig.NetworkConfig, epoch phase0.Epoch) bool {
	return !s.Liquidated && s.IsSyncCommitteeEligible(cfg, epoch)
}

// IsParticipatingAndAttesting returns true if the validator can participate in *all* SSV duties at the given epoch.
// Requirements: not liquidated and attesting.
func (s *SSVShare) IsParticipatingAndAttesting(epoch phase0.Epoch) bool {
	return !s.Liquidated && s.IsAttesting(epoch)
}

func (s *SSVShare) IsSyncCommitteeEligible(cfg networkconfig.NetworkConfig, epoch phase0.Epoch) bool {
	if s.IsAttesting(epoch) {
		return true
	}

	if s.Status.IsExited() || s.Status == eth2apiv1.ValidatorStateWithdrawalPossible || s.Status == eth2apiv1.ValidatorStateActiveSlashed {
		// if validator exited within Current Period OR Current Period - 1, then it is eligible
		// because Sync committees are assigned EPOCHS_PER_SYNC_COMMITTEE_PERIOD in advance
		if epoch >= s.ExitEpoch && cfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)-cfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(s.ExitEpoch) <= 1 {
			return true
		}
	}

	return false
}

func (s *SSVShare) SetMinParticipationEpoch(epoch phase0.Epoch) {
	s.minParticipationEpoch = epoch
}

func (s *SSVShare) MinParticipationEpoch() phase0.Epoch {
	return s.minParticipationEpoch
}

func (s *SSVShare) SetFeeRecipient(feeRecipient bellatrix.ExecutionAddress) {
	s.FeeRecipientAddress = feeRecipient
}

// CommitteeID safely retrieves or computes the CommitteeID.
func (s *SSVShare) CommitteeID() spectypes.CommitteeID {
	// Load the current value of committeeID atomically.
	if ptr := s.committeeID.Load(); ptr != nil {
		return *ptr
	}

	// Compute the CommitteeID since it's not yet set.
	ids := make([]spectypes.OperatorID, len(s.Committee))
	for i, v := range s.Committee {
		ids[i] = v.Signer
	}
	id := ComputeCommitteeID(ids)

	// Create a new pointer and store it atomically.
	s.committeeID.Store(&id)
	return id
}

func (s *SSVShare) OperatorIDs() []spectypes.OperatorID {
	ids := make([]spectypes.OperatorID, len(s.Committee))
	for i, v := range s.Committee {
		ids[i] = v.Signer
	}
	return ids
}

func (s *SSVShare) HasQuorum(cnt uint64) bool {
	return cnt >= s.Quorum()
}

func (s *SSVShare) Quorum() uint64 {
	q, _ := ComputeQuorumAndPartialQuorum(uint64(len(s.Committee)))
	return q
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

func ComputeQuorumAndPartialQuorum(committeeSize uint64) (quorum uint64, partialQuorum uint64) {
	f := ComputeF(committeeSize)
	return f*2 + 1, f + 1
}

func ComputeF(committeeSize uint64) uint64 {
	return (committeeSize - 1) / 3
}

func ValidCommitteeSize(committeeSize uint64) bool {
	f := ComputeF(committeeSize)
	return (committeeSize-1)%3 == 0 && f >= 1 && f <= 4
}

// Return a 32 bytes ID for the committee of operators
func ComputeCommitteeID(committee []spectypes.OperatorID) spectypes.CommitteeID {
	// sort
	sort.Slice(committee, func(i, j int) bool {
		return committee[i] < committee[j]
	})
	// Convert to bytes
	bytes := make([]byte, len(committee)*4)
	for i, v := range committee {
		binary.LittleEndian.PutUint32(bytes[i*4:], uint32(v)) // #nosec G115
	}
	// Hash
	return sha256.Sum256(bytes)
}
