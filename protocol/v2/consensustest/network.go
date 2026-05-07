package consensustest

import (
	mrand "math/rand"
	"time"
)

// MsgKind discriminates message kinds for the network / byz models. Not all
// kinds apply to every protocol; adapters ignore irrelevant ones.
type MsgKind int

const (
	KindLeaderBroadcast MsgKind = iota // OBFT Phase1Bundle / QBFT PROPOSE
	KindCommit                         // OBFT Commit / QBFT PREPARE+COMMIT (same mesh)
	KindRoundChange                    // QBFT-specific
	KindCertificate                    // OBFT cert gossip / QBFT decided-msg
	KindPostConsensus                  // QBFT partial-sig collection (OBFT folds this into Phase 3)
)

// NetworkModel decides per-message propagation delay. Called once per
// (sender, receiver, kind) at emission time.
type NetworkModel interface {
	Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration
}

// ConstantDelay returns D for every message.
type ConstantDelay struct{ D time.Duration }

func (c ConstantDelay) Delay(_ *mrand.Rand, _, _ OperatorID, _ MsgKind) time.Duration {
	return c.D
}

// JitteredDelay draws delay from uniform [D-Jitter, D+Jitter], clamped to ≥1ns.
// Determinism preserved: RNG draws happen on the event-loop goroutine in
// scheduling order, so (Seed, NetworkModel) → byte-identical event ordering.
type JitteredDelay struct {
	D      time.Duration
	Jitter time.Duration
}

func (j JitteredDelay) Delay(rng *mrand.Rand, _, _ OperatorID, _ MsgKind) time.Duration {
	if j.Jitter <= 0 {
		return j.D
	}
	delta := time.Duration(rng.Int63n(int64(2*j.Jitter+1))) - j.Jitter
	d := j.D + delta
	if d < time.Nanosecond {
		d = time.Nanosecond
	}
	return d
}

// PerReceiverDelay applies per-receiver overrides on top of an inner model.
// Used to simulate partitions where one or more receivers see V late.
type PerReceiverDelay struct {
	Inner     NetworkModel
	Overrides map[OperatorID]time.Duration // if present, used instead of Inner.Delay
}

func (p PerReceiverDelay) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	if d, ok := p.Overrides[to]; ok {
		return d
	}
	return p.Inner.Delay(rng, from, to, kind)
}
