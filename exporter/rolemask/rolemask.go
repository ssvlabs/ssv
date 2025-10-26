package rolemask

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Mask is a compact bitfield used to record which Beacon roles
// are scheduled for a validator at a given slot. It is intentionally
// a tiny type (one byte) to keep on‑disk schedule encoding small.
//
// Bits (LSB first):
//
//	0: ATTESTER, 1: AGGREGATOR, 2: PROPOSER, 3: SYNC_COMMITTEE,
//	4: SYNC_COMMITTEE_CONTRIBUTION
type Mask = uint8

const (
	BitAttester         Mask = 1 << 0
	BitAggregator       Mask = 1 << 1
	BitProposer         Mask = 1 << 2
	BitSyncCommittee    Mask = 1 << 3
	BitSyncContribution Mask = 1 << 4
)

// roleToBit maps supported beacon roles to their bit in the schedule mask.
var roleToBit = map[spectypes.BeaconRole]Mask{
	spectypes.BNRoleAttester:                  BitAttester,
	spectypes.BNRoleAggregator:                BitAggregator,
	spectypes.BNRoleProposer:                  BitProposer,
	spectypes.BNRoleSyncCommittee:             BitSyncCommittee,
	spectypes.BNRoleSyncCommitteeContribution: BitSyncContribution,
}

// allRoles is the canonical collection of roles represented in the mask.
// Treat as read‑only.
var allRoles = []spectypes.BeaconRole{
	spectypes.BNRoleAttester,
	spectypes.BNRoleAggregator,
	spectypes.BNRoleProposer,
	spectypes.BNRoleSyncCommittee,
	spectypes.BNRoleSyncCommitteeContribution,
}

// All returns the canonical list of roles represented in the mask.
// Do not mutate the returned slice.
func All() []spectypes.BeaconRole { return allRoles }

// AllWithBits returns the supported roles with their corresponding mask bit.
// The returned map must be treated as read‑only by callers.
func AllWithBits() map[spectypes.BeaconRole]Mask { return roleToBit }

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
