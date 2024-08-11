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

func createDuty(pk []byte, slot phase0.Slot, idx phase0.ValidatorIndex, role spectypes.RunnerRole) spectypes.Duty {
	var pkBytes [48]byte
	copy(pkBytes[:], pk)

	var testingDuty spectypes.ValidatorDuty
	var beaconRole spectypes.BeaconRole
	switch role {
	case spectypes.RoleCommittee:
		return spectestingutils.TestingCommitteeAttesterDuty(slot, []int{int(idx)})
	case spectypes.RoleAggregator:
		testingDuty = spectestingutils.TestingAggregatorDuty
		beaconRole = spectypes.BNRoleAggregator
	case spectypes.RoleProposer:
		testingDuty = *spectestingutils.TestingProposerDutyV(spec.DataVersionCapella)
		beaconRole = spectypes.BNRoleProposer
	case spectypes.RoleSyncCommitteeContribution:
		testingDuty = spectestingutils.TestingSyncCommitteeContributionDuty
		beaconRole = spectypes.BNRoleSyncCommitteeContribution
	default:
		panic("unknown role")
	}

	return &spectypes.ValidatorDuty{
		Type:                          beaconRole,
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
