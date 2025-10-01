package rolemask

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Mask is a compact bitfield of scheduled BN roles.
// Bits (LSB first):
//
//	0: ATTESTER, 1: AGGREGATOR, 2: PROPOSER, 3: SYNC_COMMITTEE, 4: SYNC_COMMITTEE_CONTRIBUTION
type Mask = uint8

const (
	BitAttester         Mask = 1 << 0
	BitAggregator       Mask = 1 << 1
	BitProposer         Mask = 1 << 2
	BitSyncCommittee    Mask = 1 << 3
	BitSyncContribution Mask = 1 << 4
)

var roleToBit = map[spectypes.BeaconRole]Mask{
	spectypes.BNRoleAttester:                  BitAttester,
	spectypes.BNRoleAggregator:                BitAggregator,
	spectypes.BNRoleProposer:                  BitProposer,
	spectypes.BNRoleSyncCommittee:             BitSyncCommittee,
	spectypes.BNRoleSyncCommitteeContribution: BitSyncContribution,
}

// All returns canonical list of roles represented in the mask.
func All() []spectypes.BeaconRole {
	return []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleProposer,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
	}
}

// BitOf returns the bit for a given role and whether it is supported.
func BitOf(role spectypes.BeaconRole) (Mask, bool) {
	b, ok := roleToBit[role]
	return b, ok
}

// Has reports whether mask contains the role.
func Has(mask Mask, role spectypes.BeaconRole) bool {
	if b, ok := BitOf(role); ok {
		return mask&b != 0
	}
	return false
}
