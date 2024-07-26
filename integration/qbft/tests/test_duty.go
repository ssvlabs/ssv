package tests

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

const (
	NoDelay     = time.Duration(0)
	DefaultSlot = phase0.Slot(spectestingutils.TestingDutySlot) //ZeroSlot
)

type DutyProperties struct {
	Slot           phase0.Slot
	ValidatorIndex phase0.ValidatorIndex
	Delay          time.Duration
}

func createDuty(pk []byte, slot phase0.Slot, idx phase0.ValidatorIndex, role spectypes.BeaconRole) spectypes.Duty {
	var pkBytes [48]byte
	copy(pkBytes[:], pk)

	var testingDuty spectypes.ValidatorDuty
	switch role {
	case spectypes.BNRoleAttester:
		return spectestingutils.TestingCommitteeAttesterDuty(slot, []int{int(idx)})
	case spectypes.BNRoleAggregator:
		testingDuty = spectestingutils.TestingAggregatorDuty
	case spectypes.BNRoleProposer:
		testingDuty = *spectestingutils.TestingProposerDutyV(spec.DataVersionCapella)
	case spectypes.BNRoleSyncCommittee:
		return spectestingutils.TestingCommitteeSyncCommitteeDuty(slot, []int{int(idx)})
	case spectypes.BNRoleSyncCommitteeContribution:
		testingDuty = spectestingutils.TestingSyncCommitteeContributionDuty
	default:
		panic("unknown role")
	}

	return &spectypes.ValidatorDuty{
		Type:                          role,
		PubKey:                        pkBytes,
		Slot:                          slot,
		ValidatorIndex:                idx,
		CommitteeIndex:                testingDuty.CommitteeIndex,
		CommitteesAtSlot:              testingDuty.CommitteesAtSlot,
		CommitteeLength:               testingDuty.CommitteeLength,
		ValidatorCommitteeIndex:       testingDuty.ValidatorCommitteeIndex,
		ValidatorSyncCommitteeIndices: testingDuty.ValidatorSyncCommitteeIndices,
	}
}
