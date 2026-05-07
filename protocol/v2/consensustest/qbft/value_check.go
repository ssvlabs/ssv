package qbft

import (
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// virtualValueChecker bridges qbft.Instance's ValueChecker to the framework's
// HostPattern. CheckValue is called from inside Instance.uponProposal
// (specifically from isProposalJustification) BEFORE the Instance bumps its
// State.Round to the new message's round — so reading Instance.State.Round
// reports the OLD round, not the round of the in-flight proposal. To pass
// the correct round to the host, the sim stashes the in-flight message's
// round in `inflightRound[op]` immediately before ProcessMsg and clears it
// after; the checker reads from there. Single-goroutine DES guarantees no
// interleaving between sim's stash and the checker's read.
//
// Fallback to Instance.State.Round if no in-flight stash is set (covers
// CheckValue calls that aren't from a message-arrival event, e.g. the
// one Instance.Start makes against the start value).
type virtualValueChecker struct {
	sim *sim
	op  ct.OperatorID
}

func newVirtualValueChecker(s *sim, op ct.OperatorID) *virtualValueChecker {
	return &virtualValueChecker{sim: s, op: op}
}

func (v *virtualValueChecker) CheckValue(value []byte) error {
	if v.sim.cfg.Host == nil {
		return nil
	}
	round := v.sim.currentRoundForOp(v.op)
	frameworkRound := round - 1
	if frameworkRound < 0 {
		frameworkRound = 0
	}
	if !v.sim.cfg.Host.Validate(v.op, frameworkRound, value, ct.PhaseDecide) {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode,
			fmt.Sprintf("host validation rejected value at op=%d round=%d", v.op, round))
	}
	return nil
}

// currentRoundForOp returns the round of the in-flight message being
// processed for `op`, or the operator's current State.Round if no message is
// in flight (e.g., Instance.Start's initial CheckValue call).
func (s *sim) currentRoundForOp(op ct.OperatorID) int {
	if r, ok := s.inflightRound[spectypes.OperatorID(op)]; ok {
		return int(r)
	}
	if inst := s.instances[spectypes.OperatorID(op)]; inst != nil && inst.State != nil {
		if inst.State.Round >= specqbft.FirstRound {
			return int(inst.State.Round)
		}
	}
	return int(specqbft.FirstRound)
}
