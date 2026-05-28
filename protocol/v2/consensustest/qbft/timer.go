package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// virtualRoundTimer implements ssv.QBFTRoundTimer (and specqbft.Timer) under
// virtual time. TimeoutForRound enqueues an evtRoundTimeout into the DES
// queue at `now + RT` for round 1, or `now + RT + RTRecoveryExtra` for
// rounds ≥ 2 (the recovery-round round-change hop); later TimeoutForRound
// calls invalidate prior timers via the seq counter (callbacks no-op on
// seq mismatch). When the desConfig is configured with LastRoundUnbounded
// (QBFT-NR family), TimeoutForRound skips the schedule for the final
// round so the instance sits in it until decision or sim-end.
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
	if t.sim.cfg.LastRoundUnbounded && int(round) >= t.sim.cfg.MaxRounds {
		// QBFT-NR: round == MaxRounds is configured unbounded. Don't arm
		// a timer — the instance stays in this round until decision or
		// until the DES's RunUntil cap fires (RelayCutoff + RT +
		// RTRecoveryExtra, see des.go); ClipLateDecision in adapter.go
		// then converts unfinished sims to MISS at RelayCutoff. The seq
		// bump above is preserved so any future code path that armed a
		// timer for a prior round is invalidated, even though no such
		// path exists in practice today.
		return
	}
	// Round 1 = base RT; rounds ≥ 2 add RTRecoveryExtra (the ROUND_CHANGE
	// hop before the new leader can PROPOSE). See adapter.go for the floor.
	timeout := t.sim.cfg.RT
	if round > specqbft.FirstRound {
		timeout += t.sim.cfg.RTRecoveryExtra
	}
	t.sim.Schedule(t.sim.Now()+timeout, &evtRoundTimeout{
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
