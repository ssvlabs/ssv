package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// virtualRoundTimer implements ssv.QBFTRoundTimer (and specqbft.Timer) under
// virtual time. TimeoutForRound enqueues an evtRoundTimeout into the DES
// queue at `now + RT`; later TimeoutForRound calls invalidate prior timers
// via the seq counter (callbacks no-op on seq mismatch).
//
// Cluster construction wires one virtualRoundTimer per (operator, instance);
// each instance's Stop is invoked when the instance terminates (decided or
// marked irrelevant) — virtual-time teardown is a no-op since the DES loop
// just won't fire stale timers.
type virtualRoundTimer struct {
	sim *sim
	op  spectypes.OperatorID
	seq int64
}

func newVirtualRoundTimer(s *sim, op spectypes.OperatorID) *virtualRoundTimer {
	return &virtualRoundTimer{sim: s, op: op}
}

func (t *virtualRoundTimer) TimeoutForRound(round specqbft.Round) {
	t.seq++
	mySeq := t.seq
	t.sim.schedule(t.sim.now+t.sim.cfg.RT, &evtRoundTimeout{
		op:    t.op,
		round: round,
		mySeq: mySeq,
	})
}

func (t *virtualRoundTimer) Stop() {
	// Bumping seq invalidates any pending timeout — the next evtRoundTimeout
	// callback compares seq against the timer's current value and no-ops on
	// mismatch.
	t.seq++
}
