package consensustest

import (
	mrand "math/rand"
	"time"
)

// MsgKind discriminates message kinds for the network / byz / bandwidth models.
// Not all kinds apply to every protocol; adapters ignore irrelevant ones.
type MsgKind int

const (
	KindLeaderBroadcast MsgKind = iota // OBFT Phase1Bundle / QBFT PROPOSE
	KindCommit                         // OBFT Commit / QBFT PREPARE+COMMIT (same mesh)
	KindRoundChange                    // QBFT-specific
	KindCertificate                    // OBFT cert gossip / QBFT decided-msg
	KindPostConsensus                  // QBFT partial-sig collection (OBFT folds this into Phase 3)
)

// String returns a stable name suitable for telemetry / report keys.
func (k MsgKind) String() string {
	switch k {
	case KindLeaderBroadcast:
		return "LeaderBroadcast"
	case KindCommit:
		return "Commit"
	case KindRoundChange:
		return "RoundChange"
	case KindCertificate:
		return "Certificate"
	case KindPostConsensus:
		return "PostConsensus"
	default:
		return "Unknown"
	}
}

// NetworkModel decides per-message propagation delay. Called once per
// (sender, receiver, kind) at emission time. Returning a delay > some large
// constant simulates an effectively dropped message (propagation past
// slot end).
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

// PartitionedNetwork drops messages whose receiver is in Partitioned. Inner
// supplies the delay for non-partitioned receivers. Use a sustained sim-long
// time as the dropped delay (1h) so the message effectively never arrives.
type PartitionedNetwork struct {
	Inner       NetworkModel
	Partitioned map[OperatorID]bool
}

func (p PartitionedNetwork) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	if p.Partitioned[to] {
		return time.Hour
	}
	return p.Inner.Delay(rng, from, to, kind)
}

// ClockSkewedNetwork models per-operator clock skew at the network layer:
// each op has a virtual-clock offset Skew[op] (positive = clock ahead,
// negative = behind). The effective propagation delay from op_s to op_r is
// `Inner.Delay + Skew[op_r] - Skew[op_s]` — i.e., only the *relative* skew
// between sender and receiver shifts when the message effectively appears
// in receiver's frame. This matches the spec's δ-bound semantics: per-pair
// clock differences ≤ δ; same skew on both sides cancels.
//
// Equivalent abstraction (without modeling DES timer firings per-op): if op_s
// has clock δ_s ahead, it fires its T_commit timer at global_time =
// T_commit_local − δ_s, so its message arrives sooner in global time; if op_r
// has clock δ_r ahead, its T_commit threshold is reached sooner in global,
// so an arrival at global_time t is interpreted as op_r-local = t + δ_r.
// The relative offset δ_r − δ_s is what the protocol perceives.
//
// Convention: skew typically drawn within ±δ (the spec's per-cluster bound,
// 50ms at Config A) to verify the protocol tolerates the partial-synchrony
// δ-bound. Out-of-bound skew (|δ_s − δ_r| > 2·δ) exceeds the spec's tolerated
// range and the protocol may legitimately miss.
type ClockSkewedNetwork struct {
	Inner NetworkModel
	Skew  map[OperatorID]time.Duration // per-op clock offset; missing op → 0
}

func (c ClockSkewedNetwork) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	base := c.Inner.Delay(rng, from, to, kind)
	delay := base + c.Skew[to] - c.Skew[from]
	if delay < time.Nanosecond {
		delay = time.Nanosecond
	}
	return delay
}
