package consensustest

import (
	"math"
	mrand "math/rand"
	"sync"
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

// LogNormalDelay draws delays from a log-normal distribution — a more
// production-realistic model than JitteredDelay's uniform jitter. Real
// gossipsub propagation has a heavy right tail (P99/P50 ratio of ~3-10x
// in mainnet conditions); log-normal is the standard fit for that shape.
//
// Parameters:
//   - Median: the 50th-percentile delay, exp(μ) of the underlying normal.
//   - Sigma:  log-space stddev (dimensionless). Larger Sigma → fatter tail.
//     Calibration: Sigma=0.3 produces P99/P50 ≈ 2.1×; Sigma=0.5 ≈ 3.6×;
//     Sigma=0.7 ≈ 5.2×. SSV mainnet gossipsub observations would let
//     deployments dial this; the framework defaults are illustrative.
//
// CALIBRATE: Median should match observed SSV mainnet propagation P50 per
// message kind (proposer-duty leader broadcast P50 ≈ 100ms based on community
// telemetry; not size-aware in this model — see SizedDelay note in plan).
// Sigma should be fit from the observed P99/P50 ratio in the same telemetry.
//
// Stateless; safe to share across sims. Determinism preserved via the
// passed-in seeded *math/rand.Rand.
type LogNormalDelay struct {
	Median time.Duration
	Sigma  float64
}

func (l LogNormalDelay) Delay(rng *mrand.Rand, _, _ OperatorID, _ MsgKind) time.Duration {
	if l.Median <= 0 {
		return time.Nanosecond
	}
	mu := math.Log(float64(l.Median))
	z := rng.NormFloat64()
	d := time.Duration(math.Exp(mu + l.Sigma*z))
	if d < time.Nanosecond {
		d = time.Nanosecond
	}
	return d
}

// LossyNetwork models bursty stochastic packet loss via a two-state Markov
// chain on top of an Inner network model. Captures the real-world pattern
// where loss isn't independent-Bernoulli per message but clusters in bursts
// during mesh churn / peer-score punishments / brief congestion spikes.
//
// State machine (per LossyNetwork instance, network-wide):
//   - good state: probability `a = LossRate / (BurstFactor · (1-LossRate))`
//     of transitioning to bad per call.
//   - bad state:  probability `1/BurstFactor` of recovering to good per call;
//     while bad, every message is dropped (returns the sentinel 1·time.Hour
//     delay matching PartitionedNetwork's drop convention).
//
// Steady-state P(bad) = LossRate; mean dwell time in bad = BurstFactor
// messages. BurstFactor=1 recovers memoryless Bernoulli; BurstFactor=10
// produces ~10-message bursts of consecutive drops.
//
// CALIBRATE: LossRate from observed SSV mainnet message-loss telemetry
// (likely 0.1-1% under healthy conditions, spikes during peer-score events).
// BurstFactor from observed dwell-time in lossy periods (~5-20 messages
// based on typical gossipsub mesh-churn windows).
//
// Stateful: holds a single network-wide (good/bad) state with a mutex.
// CONSTRUCT FRESH PER SIM via NewLossyNetwork — sharing one instance
// across multiple parallel sims would cross-contaminate state and break
// determinism. For batch usage, instantiate inside Scenario.Apply.
//
// For per-PAIR sustained flakiness (a specific link is bad while others
// are fine), use CorrelatedLinkDelay instead. The two are composable
// (wrap CorrelatedLinkDelay inside LossyNetwork or vice versa).
type LossyNetwork struct {
	Inner       NetworkModel
	LossRate    float64
	BurstFactor int

	mu     sync.Mutex
	state  byte // 0=good, 1=bad
	inited bool
}

// NewLossyNetwork constructs a LossyNetwork with fresh Markov state.
// Use one per sim to preserve per-sim determinism (state across sims
// would cross-contaminate otherwise).
func NewLossyNetwork(inner NetworkModel, lossRate float64, burstFactor int) *LossyNetwork {
	return &LossyNetwork{
		Inner:       inner,
		LossRate:    lossRate,
		BurstFactor: burstFactor,
	}
}

// DroppedDelay is the sentinel duration used by LossyNetwork (and
// PartitionedNetwork) to signal a dropped message. Receivers won't observe
// it within the slot's relay-submission deadline; downstream Resolve treats
// it as never-arrived.
const DroppedDelay = time.Hour

func (l *LossyNetwork) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	l.mu.Lock()
	// Fast-path edge cases first (avoid wasting RNG draws).
	if l.LossRate <= 0 {
		l.mu.Unlock()
		return l.Inner.Delay(rng, from, to, kind)
	}
	if l.LossRate >= 1 {
		l.mu.Unlock()
		return DroppedDelay
	}
	if l.BurstFactor < 1 {
		l.BurstFactor = 1 // protect against misconfiguration; geometric (memoryless) loss
	}

	// Initial state: weighted by steady-state P(bad) = LossRate.
	if !l.inited {
		if rng.Float64() < l.LossRate {
			l.state = 1
		}
		l.inited = true
	}

	// Markov step.
	if l.state == 0 {
		// good → bad with prob a = LossRate / (BurstFactor · (1 - LossRate))
		a := l.LossRate / (float64(l.BurstFactor) * (1 - l.LossRate))
		if rng.Float64() < a {
			l.state = 1
		}
	} else {
		// bad → good with prob 1/BurstFactor.
		if rng.Float64() < 1.0/float64(l.BurstFactor) {
			l.state = 0
		}
	}

	bad := l.state == 1
	l.mu.Unlock()

	if bad {
		return DroppedDelay
	}
	return l.Inner.Delay(rng, from, to, kind)
}

// CorrelatedLinkDelay models per-pair sustained flakiness — the
// real-world pattern where one specific link between two operators
// stays slow for a window of messages (the sender's NIC is hot, the
// receiver's CPU is loaded, the path between them is congested, etc.)
// while OTHER links in the cluster behave normally. Distinct from
// LossyNetwork's network-wide bursts.
//
// State machine (per-pair Markov chain over good/bad):
//   - good: probability a = BadLinkProb / (BurstMessages · (1-BadLinkProb))
//     of transitioning to bad per call on this link.
//   - bad:  probability 1/BurstMessages of recovering per call; while
//     bad, the link's delay is Inner.Delay × BadLinkMultiplier (not
//     dropped — sustained-slow, not unreachable).
//
// Steady-state P(bad) per pair = BadLinkProb. Pairs are independent
// (each pair's Markov chain is independent of every other pair's).
// BadLinkMultiplier=1 reduces to Inner; =3 means bad links deliver in
// 3x the baseline propagation time.
//
// CALIBRATE: BadLinkProb from observed fraction of operator pairs that
// exhibit sustained-slow delivery in SSV mainnet (typical ~5-20% of
// pairs based on peer-score telemetry). BadLinkMultiplier from the
// P50-of-bad-pairs / P50-of-good-pairs ratio. BurstMessages from the
// observed dwell time in bad-pair status (~10-50 messages per typical
// mesh-churn window).
//
// Stateful: per-pair state map with a mutex. CONSTRUCT FRESH PER SIM
// via NewCorrelatedLinkDelay — sharing across sims would cross-
// contaminate per-pair state.
type CorrelatedLinkDelay struct {
	Inner             NetworkModel
	BadLinkProb       float64
	BadLinkMultiplier float64
	BurstMessages     int

	mu       sync.Mutex
	linkBad  map[linkKey]bool
	linkSeen map[linkKey]bool
}

type linkKey struct {
	from, to OperatorID
}

// NewCorrelatedLinkDelay constructs a CorrelatedLinkDelay with fresh
// per-pair state. Use one per sim to preserve determinism.
func NewCorrelatedLinkDelay(inner NetworkModel, badLinkProb, badLinkMultiplier float64, burstMessages int) *CorrelatedLinkDelay {
	return &CorrelatedLinkDelay{
		Inner:             inner,
		BadLinkProb:       badLinkProb,
		BadLinkMultiplier: badLinkMultiplier,
		BurstMessages:     burstMessages,
		linkBad:           make(map[linkKey]bool),
		linkSeen:          make(map[linkKey]bool),
	}
}

func (c *CorrelatedLinkDelay) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	pair := linkKey{from: from, to: to}

	c.mu.Lock()
	if c.linkBad == nil {
		c.linkBad = make(map[linkKey]bool)
		c.linkSeen = make(map[linkKey]bool)
	}
	if c.BurstMessages < 1 {
		c.BurstMessages = 1
	}

	// Lazy per-pair init at first observation: weighted by steady-state
	// P(bad) = BadLinkProb.
	if !c.linkSeen[pair] {
		if rng.Float64() < c.BadLinkProb {
			c.linkBad[pair] = true
		}
		c.linkSeen[pair] = true
	}

	// Per-pair Markov step.
	if !c.linkBad[pair] {
		// good → bad with prob a.
		a := c.BadLinkProb / (float64(c.BurstMessages) * (1 - c.BadLinkProb))
		if c.BadLinkProb >= 1 {
			a = 1
		}
		if rng.Float64() < a {
			c.linkBad[pair] = true
		}
	} else {
		// bad → good with prob 1/BurstMessages.
		if rng.Float64() < 1.0/float64(c.BurstMessages) {
			c.linkBad[pair] = false
		}
	}
	bad := c.linkBad[pair]
	c.mu.Unlock()

	base := c.Inner.Delay(rng, from, to, kind)
	if bad {
		return time.Duration(float64(base) * c.BadLinkMultiplier)
	}
	return base
}
