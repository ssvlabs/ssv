// Package ssvtestingutils holds SSV-side helpers used only by tests.
package ssvtestingutils

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// NewMsgID builds a spectypes.MessageID from a domain, an arbitrary-length duty-executor id and a
// role, right-aligning the executor id in the executor slot.
//
// It reproduces the removed spectypes.NewMsgID (ssv-spec split it into the fixed-size
// NewValidatorMsgID/NewCommitteeMsgID, which production code now uses). Tests still need the
// length-agnostic form to build synthetic or malformed ids whose executor is neither a full
// validator pubkey nor a committee id; for those two sizes the output is byte-identical to the
// typed constructors. One deliberate divergence: an executor id longer than the slot keeps only
// its leading 48 bytes, where the removed constructor would have overflowed into the role bytes
// — behavior no test relied on.
func NewMsgID(domain spectypes.DomainType, dutyExecutorID []byte, role spectypes.RunnerRole) spectypes.MessageID {
	// Delegate the domain+role bytes (with a zeroed executor slot) to the typed constructor, then
	// right-align the arbitrary-length executor id in that slot, as the removed spectypes.NewMsgID did.
	mid := spectypes.NewValidatorMsgID(domain, spectypes.ValidatorPK{}, role)

	execEnd := len(mid)
	execStart := execEnd - len(spectypes.ValidatorPK{})
	start := execEnd - len(dutyExecutorID)
	if start < execStart {
		start = execStart
	}
	copy(mid[start:execEnd], dutyExecutorID)

	return mid
}
