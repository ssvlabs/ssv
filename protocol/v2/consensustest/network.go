package consensustest

import (
	"fmt"
	"math"
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

// MarkovianSlownessDelay models slow peer links via a per-link two-state
// Markov chain (slow / fast). Each undirected link pair (from, to) involving
// a slow op gets its own independent chain, so op2→op3 and op2→op4 degrade
// independently. For every message touching a slow op (from-side or to-side):
//   - The chain stays in its current state with probability PersistP;
//     it flips with probability (1 − PersistP). In the slow state the
//     call returns Inner.Delay + ExtraDelay (additive); in the fast state
//     it falls through to Inner.Delay.
//   - Each link starts in the slow state by design: the very first
//     message on that link returns Inner.Delay + ExtraDelay unconditionally
//     (no transition roll on call #1). This is a deliberate asymmetric
//     initial condition vs the symmetric steady state — it encodes
//     the spec assumption that the slot starts mid-bad-period rather
//     than at a random point in the chain's stationary distribution.
//     Practical consequence: at low message counts per link, the
//     observed slow fraction sits above 50% (the steady-state value);
//     for a typical slot's worth of messages it converges to 50%.
//
// This is a genuine two-state Markov chain (not memoryless Bernoulli):
// the next-message outcome depends on the current state, so runs of
// consecutive slow-state messages cluster — matching real-world bursts
// where networking problems persist across stretches of consecutive
// messages instead of toggling per-packet.
//
// At PersistP = 0.8 the steady-state is 50/50 slow/fast (symmetric
// chain), with mean dwell time 1/(1−PersistP) = 5 messages in each
// state. Raising PersistP lengthens dwell times in both states without
// changing the steady-state fraction; lowering toward 0.5 weakens the
// burst structure toward independent Bernoulli; PersistP = 0 makes
// every call flip and is degenerate (don't do this).
//
// Both directions of the same link share state (via undirected pair key)
// so inbound and outbound slowness are coupled, matching how peer-link
// issues behave in practice. Different links involving the same slow op
// have independent chains. State (one entry per undirected pair) lives
// on the struct. Construct a fresh instance per iteration to avoid
// cross-iteration contamination — scenarios.Apply should build a new
// MarkovianSlownessDelay each time.
type MarkovianSlownessDelay struct {
	Inner      NetworkModel
	SlowOps    map[OperatorID]bool
	ExtraDelay time.Duration
	PersistP   float64
	State      map[linkKey]*markovSlownessState
}

// markovSlownessState carries the chain state for one link pair. slow=true
// means the next call returns Inner.Delay + ExtraDelay (subject to a
// PersistP/flip roll on every call AFTER initialization); slow=false falls
// through to Inner.Delay. inited marks the first observation so the chain
// enters in the slow state (no transition roll on call #1).
type markovSlownessState struct {
	inited bool
	slow   bool
}

func (m MarkovianSlownessDelay) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	if !m.SlowOps[from] && !m.SlowOps[to] {
		return m.Inner.Delay(rng, from, to, kind)
	}
	pair := newLinkKey(from, to)
	state := m.State[pair]
	if state == nil {
		state = &markovSlownessState{}
		m.State[pair] = state
	}
	if !state.inited {
		state.inited = true
		state.slow = true // spec: slot starts mid-bad-period
	} else if rng.Float64() >= m.PersistP {
		state.slow = !state.slow
	}
	if state.slow {
		return m.Inner.Delay(rng, from, to, kind) + m.ExtraDelay
	}
	return m.Inner.Delay(rng, from, to, kind)
}

// NewMarkovianSlowness constructs a fresh MarkovianSlownessDelay with an
// initialized state map (per-iteration init — see the type doc).
func NewMarkovianSlowness(inner NetworkModel, slowOps []OperatorID, extra time.Duration, persistP float64) MarkovianSlownessDelay {
	set := make(map[OperatorID]bool, len(slowOps))
	for _, op := range slowOps {
		set[op] = true
	}
	return MarkovianSlownessDelay{
		Inner:      inner,
		SlowOps:    set,
		ExtraDelay: extra,
		PersistP:   persistP,
		State:      make(map[linkKey]*markovSlownessState),
	}
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

// ClockSkewedNetwork models per-operator clock skew as a RELATIVE-DELAY
// proxy at the network layer. Each op has a virtual-clock offset Skew[op]
// (positive = clock ahead, negative = behind). The effective propagation
// delay from op_s to op_r is `Inner.Delay + Skew[op_r] − Skew[op_s]` — the
// *relative* skew between sender and receiver shifts when the message
// effectively appears in the receiver's frame.
//
// What this model captures:
//   - Per-pair relative-skew effects on cluster-wide propagation timing
//     (e.g., sender clock-ahead reaches receiver "earlier" in global
//     time; receiver clock-ahead interprets arrival as "later" locally).
//   - The spec's δ-bound semantics: per-pair clock differences ≤ δ; same
//     skew on both sides cancels.
//
// What this model does NOT capture:
//   - Independent per-operator LOCAL T_commit timer firings. The DES
//     fires a single global T_commit event for all ops; each op's
//     Phase-1 acceptance cutoff and Phase-2 emission happen in lockstep
//     in global time, not per-op clock time.
//   - Per-op Phase-3 RoundEndOffset divergence under skew.
//   - Skew-driven Phase-1-acceptance edge cases where receiver's local
//     T_commit fires before/after the bundle's arrival on receiver's
//     local clock — currently approximated by the network-side delay
//     shift.
//
// For correctness-conformance under δ-bound, this proxy is adequate
// (TestSweep_ClockSkew exercises it across the spec's three δ-bound
// regions: within-bound, at-bound, out-of-bound). For full local-clock-
// driven DES modeling (e.g., to surface a specific operator deciding
// Phase 1 at their own local T_commit + δ), a future refactor would
// extend the DES scheduler with per-op virtual clocks.
//
// Convention: skew typically drawn within ±δ (the spec's per-cluster bound,
// 50ms at Config A). Out-of-bound skew (|δ_s − δ_r| > 2·δ) exceeds the
// spec's tolerated range and the protocol may legitimately miss.
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

// LogNormalDelay draws delays from a log-normal distribution — the
// production-shaped propagation model used as the stress-tier baseline.
// Real gossipsub propagation has a heavy right tail (P99/P50 ratio of
// ~3-10× in mainnet conditions); log-normal is the standard fit for
// that shape.
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
	raw := math.Exp(mu + l.Sigma*z)
	// Clamp before int64 conversion: math.Exp can return +Inf at extreme
	// tail draws (z·Sigma ≳ 44 at Median≈1s), and time.Duration(+Inf) is
	// implementation-defined — typically MinInt64, which downstream code
	// would interpret as a NEGATIVE duration. DroppedDelay is the sentinel
	// the framework already uses for "doesn't arrive within the slot", so
	// extreme-tail draws collapse to the same semantic as PartitionedNetwork
	// / LossyNetwork: the receiver simply never sees the message in time.
	if raw > float64(DroppedDelay) || math.IsNaN(raw) || math.IsInf(raw, 1) {
		return DroppedDelay
	}
	d := time.Duration(raw)
	if d < time.Nanosecond {
		d = time.Nanosecond
	}
	return d
}

// LogNormalMixtureDelay draws delays from a k-component lognormal mixture
// — a strict generalization of LogNormalDelay that captures the heavier
// upper tail seen in real SSV mainnet gossipsub data.
//
// Empirical fact (24h prod-mainnet cluster, COMMITTEE-1_2_3_4, 189,686
// cross-op send→recv pairs, May 2026): a single lognormal under-fits the
// tail dramatically — p99 by ~22%, p99.9 by ~73%, p99.99 by ~85%. A
// 3-component lognormal mixture fits every quantile from p50 through
// p99.9 within ~4%. See Prod_1_2_3_4_CalibratedLogNormalMixture /
// Stage_53_54_55_56_CalibratedLogNormalMixture /
// Stage_97_98_99_100_CalibratedLogNormalMixture for the fitted
// parameters and tools/scout for the analysis used to derive them.
//
// Model:
//
//	X ~ Σ_i π_i · LogNormal(μ_i, σ_i)     with Σ π_i = 1
//
// Sampling: pick component i with probability π_i, then draw
//
//	X = exp(μ_i + σ_i · Z),  Z ~ N(0, 1)
//
// Parameters per component:
//   - Weight: π_i, the mixture weight. The constructor builds an
//     internal normalized cumulative-weight table (cumWeights) used at
//     sampling time; the public Components slice retains the raw
//     weights as the caller supplied them (not mutated). Callers may
//     pass weights summing to any positive value — the per-component
//     fractions used at sampling are weight_i / Σ weight.
//   - Median: exp(μ_i), the per-component median.
//   - Sigma:  σ_i, log-space stddev of component i.
//
// Stateless apart from the passed-in *math/rand.Rand; safe to share
// across sims. Determinism preserved via the seeded rng. Like
// LogNormalDelay, extreme-tail draws collapse to DroppedDelay rather
// than overflowing time.Duration.
type LogNormalMixtureDelay struct {
	Components []LogNormalComponent
	// cumWeights is built once by NewLogNormalMixtureDelay; reading it
	// is data-race-free as long as no caller mutates Components after
	// construction.
	cumWeights []float64
}

type LogNormalComponent struct {
	Weight float64       // π_i, mixture weight
	Median time.Duration // exp(μ_i)
	Sigma  float64       // σ_i, log-space stddev
}

// NewLogNormalMixtureDelay normalizes weights and precomputes the
// cumulative weight table used for component selection. Panics if
// no components are supplied (an empty mixture is not a valid model).
func NewLogNormalMixtureDelay(components []LogNormalComponent) *LogNormalMixtureDelay {
	if len(components) == 0 {
		panic("LogNormalMixtureDelay requires at least one component")
	}
	var sum float64
	for _, c := range components {
		if c.Weight < 0 {
			panic("LogNormalMixtureDelay: component weights must be non-negative")
		}
		sum += c.Weight
	}
	if sum <= 0 {
		panic("LogNormalMixtureDelay: component weights sum to zero")
	}
	cum := make([]float64, len(components))
	running := 0.0
	for i, c := range components {
		running += c.Weight / sum
		cum[i] = running
	}
	// Guard against floating drift on the last entry.
	cum[len(cum)-1] = 1.0
	return &LogNormalMixtureDelay{Components: components, cumWeights: cum}
}

func (l *LogNormalMixtureDelay) Delay(rng *mrand.Rand, _, _ OperatorID, _ MsgKind) time.Duration {
	// Pick a component by inverse-CDF on the cumulative weights.
	u := rng.Float64()
	idx := 0
	for i, c := range l.cumWeights {
		if u <= c {
			idx = i
			break
		}
	}
	c := l.Components[idx]
	if c.Median <= 0 {
		return time.Nanosecond
	}
	mu := math.Log(float64(c.Median))
	z := rng.NormFloat64()
	raw := math.Exp(mu + c.Sigma*z)
	// Same overflow guard as LogNormalDelay: clamp extreme tail draws to
	// DroppedDelay so time.Duration(+Inf) can't silently become a negative
	// duration downstream.
	if raw > float64(DroppedDelay) || math.IsNaN(raw) || math.IsInf(raw, 1) {
		return DroppedDelay
	}
	d := time.Duration(raw)
	if d < time.Nanosecond {
		d = time.Nanosecond
	}
	return d
}

// Prod_1_2_3_4_CalibratedLogNormalMixture returns the 3-component lognormal
// mixture fitted to 24h of SSV prod-mainnet `COMMITTEE-1_2_3_4` cross-op
// QBFT propagation latency data (proposal+prepare+commit pooled,
// n=189,686 sender→receiver pairs sampled 2026-05-12 11:00Z → 2026-05-13 11:00Z).
//
// Empirical quantile reproduction within: p50 +0.3%, p90 +1.0%, p95 +0.1%,
// p99 +3.6%, p99.9 −2.1%, p99.99 +14% (well within tail-sampling noise
// at this quantile).
func Prod_1_2_3_4_CalibratedLogNormalMixture() *LogNormalMixtureDelay {
	// Medians are stored as integer microseconds (sub-µs precision is
	// well below clock-skew noise floor; keeping ints removes any
	// Duration-rounding ambiguity).
	return NewLogNormalMixtureDelay([]LogNormalComponent{
		{Weight: 0.7794, Median: 1303 * time.Microsecond, Sigma: 0.3774},
		{Weight: 0.0805, Median: 1458 * time.Microsecond, Sigma: 1.4909},
		{Weight: 0.1400, Median: 3457 * time.Microsecond, Sigma: 0.4140},
	})
}

// Stage_53_54_55_56_CalibratedLogNormalMixture returns the 3-component lognormal mixture
// fitted to 24h of SSV stage-hoodi `COMMITTEE-53_54_55_56` cross-op QBFT
// propagation latency data (proposal+prepare+commit pooled, n=342,030
// sender→receiver pairs sampled 2026-05-13 11:00Z → 2026-05-14 10:30Z).
//
// Empirical quantile reproduction within: p50 +1.4%, p90 +0.9%, p95 +1.2%,
// p99 −0.05%, p99.9 +12%; p99.99 is under-predicted by ~65% because the
// stage extreme tail is dominated by a handful of multi-second events
// (likely QBFT round-change timeouts) that are not lognormal.
func Stage_53_54_55_56_CalibratedLogNormalMixture() *LogNormalMixtureDelay {
	return NewLogNormalMixtureDelay([]LogNormalComponent{
		{Weight: 0.0905, Median: 318 * time.Microsecond, Sigma: 1.6463},
		{Weight: 0.3759, Median: 1084 * time.Microsecond, Sigma: 0.7824},
		{Weight: 0.5337, Median: 2581 * time.Microsecond, Sigma: 0.4030},
	})
}

// Stage_97_98_99_100_CalibratedLogNormalMixture is fit to 24h of stage-hoodi
// `COMMITTEE-97_98_99_100` cross-op QBFT propagation latency (n=342,409
// pairs sampled 2026-05-13 10:00Z → 2026-05-14 10:30Z).
//
// Empirical fit (3-component): p50 +1.9%, p90 −0.6%, p99 −10.5%,
// p99.9 +86%, p99.99 +117%. The 3-component fit struggles above p99
// on this cluster because the tail is composed of bandwidth-bound
// proposer events (p99 = 40 ms) that exceed the lognormal-mixture
// regime. If your scenario specifically cares about the p99–p99.9
// region here, consider the 4-component refit logged in
// tools/scout/analysis/output/stage-hoodi_97-100_mixture3_fit.json.
func Stage_97_98_99_100_CalibratedLogNormalMixture() *LogNormalMixtureDelay {
	return NewLogNormalMixtureDelay([]LogNormalComponent{
		{Weight: 0.1379, Median: 976 * time.Microsecond, Sigma: 1.9473},
		{Weight: 0.4049, Median: 2261 * time.Microsecond, Sigma: 0.9452},
		{Weight: 0.4573, Median: 3519 * time.Microsecond, Sigma: 0.3800},
	})
}

// P2PProfileNames lists the calibrated mesh-hop-delay profiles exposed
// to the stress driver and to the report UI. Order is load-bearing: the
// index is what gets stored in the per-point Fields["p2p_profile"]
// float64 (the payload's Fields map is float64-valued, so profile
// identity is encoded as a stable index into this slice). Keep the
// order stable across releases; new profiles append at the end.
//
// Six profiles:
//   - prod / stage1 / stage2: the three empirically-fitted mixtures
//     from Prod_1_2_3_4_*, Stage_53_54_55_56_*, Stage_97_98_99_100_*.
//   - slow: prod with each component median ×20 (same shape, shifted
//     right by 20× — models a uniformly much-slower-than-prod mesh).
//   - heavy_tail: prod with each component σ scaled so the probability
//     of drawing above the original P99 is 12× (medians unchanged —
//     models prod with rarer events spiking much higher / more often).
//   - slow_heavy_tail: prod ∘ slow ∘ heavy_tail; uniformly slower mesh
//     with bursty rare events on top.
var P2PProfileNames = []string{
	"prod",
	"stage1",
	"stage2",
	"slow",
	"heavy_tail",
	"slow_heavy_tail",
}

// P2PProfile returns a fresh NetworkModel for the named profile. The
// returned model is per-call (LossyNetwork / CorrelatedLinkDelay-style
// stateful wrappers compose on top in sweep builders, so a fresh
// instance per sim is required for determinism). Panics on unknown
// names — names must match P2PProfileNames exactly.
func P2PProfile(name string) NetworkModel {
	switch name {
	case "prod":
		return Prod_1_2_3_4_CalibratedLogNormalMixture()
	case "stage1":
		return Stage_53_54_55_56_CalibratedLogNormalMixture()
	case "stage2":
		return Stage_97_98_99_100_CalibratedLogNormalMixture()
	case "slow":
		return Prod_1_2_3_4_CalibratedLogNormalMixture().Slowed(20)
	case "heavy_tail":
		return Prod_1_2_3_4_CalibratedLogNormalMixture().HeavyTailed(12)
	case "slow_heavy_tail":
		return Prod_1_2_3_4_CalibratedLogNormalMixture().Slowed(20).HeavyTailed(12)
	default:
		panic(fmt.Sprintf("consensustest: unknown P2P profile %q; valid: %v", name, P2PProfileNames))
	}
}

// P2PProfileIndex returns the index of `name` in P2PProfileNames. Used
// by sweep builders to encode the profile selection as a float64 in
// Fields["p2p_profile"] (the report payload's Fields map is float64-
// valued; profile identity round-trips via the index). Panics on
// unknown names so a typo in a sweep builder fails loudly rather than
// producing a silent off-by-one in the report.
func P2PProfileIndex(name string) int {
	for i, n := range P2PProfileNames {
		if n == name {
			return i
		}
	}
	panic(fmt.Sprintf("consensustest: unknown P2P profile %q; valid: %v", name, P2PProfileNames))
}

// Slowed returns a copy of `l` with each component's Median scaled by
// `factor`. Sigma values are left unchanged, so the per-component shape
// (P99/median ratio) is preserved — the whole distribution shifts right
// in time by the same factor. Used by derived profiles like "slow"
// (factor=20 over prod).
//
// Panics on factor ≤ 0 — a non-positive scaling factor would either
// invert or collapse the distribution and almost certainly signals a
// caller bug.
func (l *LogNormalMixtureDelay) Slowed(factor float64) *LogNormalMixtureDelay {
	if factor <= 0 {
		panic(fmt.Sprintf("LogNormalMixtureDelay.Slowed: factor must be > 0, got %v", factor))
	}
	out := make([]LogNormalComponent, len(l.Components))
	for i, c := range l.Components {
		out[i] = LogNormalComponent{
			Weight: c.Weight,
			Median: time.Duration(float64(c.Median) * factor),
			Sigma:  c.Sigma,
		}
	}
	return NewLogNormalMixtureDelay(out)
}

// HeavyTailed returns a copy of `l` with every component's Sigma
// scaled by the SAME factor, chosen so the mixture-level probability
// of drawing a value above the original mixture's P99 grows by
// `outlierFreqMultiplier`. Each component's median is left unchanged,
// so the mixture's overall median shifts by only a few percent.
// Used by derived profiles like "heavy_tail" (outlier-freq×12 over prod).
//
// For a single-component mixture, the math admits the closed-form
// scale σ_new = σ_old · Φ^{-1}(0.99) / Φ^{-1}(1 − k·(1−0.99)). For
// multi-component mixtures with different per-component σ_i the
// closed form doesn't generalize: applying it to each component
// independently is a single-component property, not a mixture-level
// one, and the mixture's actual outlier-frequency multiplier comes
// out far below the target (≈ 1.8 for prod when targeting 4). Instead
// the implementation:
//
//  1. Computes the original mixture's P99 (V) via bisection on the
//     analytic mixture CDF.
//  2. Binary-searches the uniform σ scale `s` such that the new
//     mixture's CDF at V equals 1 − k·(1−0.99) = 0.96 for k=4.
//
// At prod (3 components, σ_i ∈ {0.38, 1.49, 0.41}) the converged
// scale is ≈ 2.0 — higher than the single-component shortcut would
// give, because the fat (σ=1.49) component already contributes most
// of the tail mass and needs more σ-amplification to drag the mixture
// outlier frequency up to the target.
//
// Panics on k ≤ 0 or k·(1−q) ≥ 1 (the target tail mass must be in
// (0, 1)). Also panics if the bisection's lower bracket (s=1, the
// no-op) already produces CDF below the target — that case implies
// the mixture P99 derivation broke, which is a programmer error.
func (l *LogNormalMixtureDelay) HeavyTailed(outlierFreqMultiplier float64) *LogNormalMixtureDelay {
	if outlierFreqMultiplier <= 0 {
		panic(fmt.Sprintf("LogNormalMixtureDelay.HeavyTailed: multiplier must be > 0, got %v", outlierFreqMultiplier))
	}
	const refQ = 0.99
	targetTailMass := outlierFreqMultiplier * (1.0 - refQ)
	if targetTailMass >= 1.0 {
		panic(fmt.Sprintf("LogNormalMixtureDelay.HeavyTailed: multiplier %v at q=%v drives target tail mass ≥ 1", outlierFreqMultiplier, refQ))
	}
	targetCDF := 1.0 - targetTailMass

	// V = original mixture's P99. Bisection in log-space; 80 iterations
	// converge to ~10⁻²³ relative precision in ns, way under any
	// downstream tolerance.
	V := l.mixtureQuantile(refQ)
	if V <= 0 || math.IsInf(V, 0) || math.IsNaN(V) {
		panic(fmt.Sprintf("LogNormalMixtureDelay.HeavyTailed: mixture P99 is %v — degenerate input?", V))
	}

	// Uniform σ scale s ∈ (1, ∞). At s=1, CDF(V) = refQ = 0.99 by
	// construction (above the target). As s→∞, each Φ((lnV − μ)/(σ·s))
	// → Φ(0) = 0.5, so mixture CDF → 0.5, well below any practical
	// target. Bisection brackets the crossing point.
	lo, hi := 1.0, 100.0
	if cdf := l.scaledMixtureCDF(hi, V); cdf > targetCDF {
		// Sanity guard: even at s=100 the CDF is still above target.
		// Happens only at multiplier values pushing tail mass beyond
		// what σ-scaling alone can produce (relative to medians).
		panic(fmt.Sprintf("LogNormalMixtureDelay.HeavyTailed: multiplier %v unreachable; CDF stuck at %v ≥ target %v at max scale", outlierFreqMultiplier, cdf, targetCDF))
	}
	for i := 0; i < 80; i++ {
		s := (lo + hi) / 2
		if l.scaledMixtureCDF(s, V) > targetCDF {
			lo = s
		} else {
			hi = s
		}
	}
	scale := (lo + hi) / 2

	out := make([]LogNormalComponent, len(l.Components))
	for i, c := range l.Components {
		out[i] = LogNormalComponent{
			Weight: c.Weight,
			Median: c.Median,
			Sigma:  c.Sigma * scale,
		}
	}
	return NewLogNormalMixtureDelay(out)
}

// mixtureCDF returns the analytic mixture CDF at `x` (sum of weighted
// per-component lognormal CDFs, normalized by total weight). x ≤ 0
// returns 0; total weight ≤ 0 returns 0 (degenerate input, but the
// constructor already rejects that case).
func (l *LogNormalMixtureDelay) mixtureCDF(x float64) float64 {
	return l.scaledMixtureCDF(1.0, x)
}

// scaledMixtureCDF returns the mixture CDF at x after multiplying each
// component's σ by `scale`. Single helper for both the quantile
// derivation (scale=1) and the heavy-tail σ search (scale > 1).
func (l *LogNormalMixtureDelay) scaledMixtureCDF(scale, x float64) float64 {
	if x <= 0 {
		return 0
	}
	var sumWeighted, totalWeight float64
	lnX := math.Log(x)
	for _, c := range l.Components {
		if c.Median <= 0 || c.Sigma <= 0 || c.Weight <= 0 {
			continue
		}
		z := (lnX - math.Log(float64(c.Median))) / (c.Sigma * scale)
		sumWeighted += c.Weight * standardNormalCDF(z)
		totalWeight += c.Weight
	}
	if totalWeight == 0 {
		return 0
	}
	return sumWeighted / totalWeight
}

// mixtureQuantile returns x such that mixtureCDF(x) = p. Bisection in
// log-space; the search window covers 1 ns to 1 hour, which brackets
// every practical mesh-hop delay by many orders of magnitude. 80
// iterations give ~10⁻²³ relative precision.
func (l *LogNormalMixtureDelay) mixtureQuantile(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return math.Inf(1)
	}
	lo, hi := math.Log(1.0), math.Log(float64(time.Hour))
	for i := 0; i < 80; i++ {
		mid := (lo + hi) / 2
		if l.mixtureCDF(math.Exp(mid)) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return math.Exp((lo + hi) / 2)
}

// standardNormalCDF returns Φ(z), the CDF of the standard normal.
// Implemented via math.Erf so callers don't need to bring a numerics
// dependency.
func standardNormalCDF(z float64) float64 {
	return 0.5 * (1 + math.Erf(z/math.Sqrt2))
}

// LossyNetwork models bursty stochastic packet loss via a per-link two-state
// Markov chain on top of an Inner network model. Captures the real-world
// pattern where loss isn't independent-Bernoulli per message but clusters in
// bursts during mesh churn / peer-score punishments / brief congestion spikes.
// Each undirected link pair gets its own independent chain so a loss burst on
// one link does not simultaneously affect all other links.
//
// State machine (per undirected link pair):
//   - good state: probability `a = LossRate / (BurstFactor · (1-LossRate))`
//     of transitioning to bad per call.
//   - bad state:  probability `1/BurstFactor` of recovering to good per call;
//     while bad, every message is dropped (returns the sentinel 1·time.Hour
//     delay matching PartitionedNetwork's drop convention).
//
// Steady-state P(bad) = LossRate per pair; mean dwell time in bad =
// BurstFactor messages. BurstFactor=1 recovers memoryless Bernoulli;
// BurstFactor=10 produces ~10-message bursts of consecutive drops.
//
// CALIBRATE: LossRate from observed SSV mainnet message-loss telemetry
// (likely 0.1-1% under healthy conditions, spikes during peer-score events).
// BurstFactor from observed dwell-time in lossy periods (~5-20 messages
// based on typical gossipsub mesh-churn windows).
//
// Stateful: holds per-link (good/bad) Markov state, one chain per
// undirected operator pair, lazily initialized on first call. Per-sim
// freshness is the contract — sharing one instance across parallel sims
// would cross-contaminate state AND break determinism. Construct via
// NewLossyNetwork inside Scenario.Apply so each iteration gets its own
// instance and the DES (which is single-goroutine within one sim) is
// the only caller of Delay; no mutex is needed under that contract.
//
// For per-PAIR sustained flakiness (a specific link is bad while others
// are fine), use CorrelatedLinkDelay instead. The two are composable
// (wrap CorrelatedLinkDelay inside LossyNetwork or vice versa).
type LossyNetwork struct {
	Inner       NetworkModel
	LossRate    float64
	BurstFactor int

	links map[linkKey]*lossyLinkState
}

// lossyLinkState is the per-link Markov state for LossyNetwork.
type lossyLinkState struct {
	state byte // 0=good, 1=bad
}

// NewLossyNetwork constructs a LossyNetwork with fresh per-link Markov state.
// Use one per sim to preserve per-sim determinism (state across sims
// would cross-contaminate otherwise).
//
// burstFactor < 1 is normalized to 1 (geometric memoryless loss); this
// is the constructor-time normalization so callers inspecting the
// struct post-construction see the effective value, not the raw input.
func NewLossyNetwork(inner NetworkModel, lossRate float64, burstFactor int) *LossyNetwork {
	if burstFactor < 1 {
		burstFactor = 1
	}
	return &LossyNetwork{
		Inner:       inner,
		LossRate:    lossRate,
		BurstFactor: burstFactor,
		links:       make(map[linkKey]*lossyLinkState),
	}
}

// DroppedDelay is the sentinel duration used by LossyNetwork (and
// PartitionedNetwork) to signal a dropped message. Receivers won't observe
// it within the slot's relay-submission deadline; downstream Resolve treats
// it as never-arrived.
const DroppedDelay = time.Hour

func (l *LossyNetwork) Delay(rng *mrand.Rand, from, to OperatorID, kind MsgKind) time.Duration {
	// Fast-path edge cases first (avoid wasting RNG draws).
	if l.LossRate <= 0 {
		return l.Inner.Delay(rng, from, to, kind)
	}
	if l.LossRate >= 1 {
		return DroppedDelay
	}

	// Lazy per-link init: first call for a pair draws the initial state
	// from the steady-state distribution P(bad) = LossRate.
	pair := newLinkKey(from, to)
	ls := l.links[pair]
	if ls == nil {
		ls = &lossyLinkState{}
		if rng.Float64() < l.LossRate {
			ls.state = 1
		}
		l.links[pair] = ls
	}

	// Markov step.
	if ls.state == 0 {
		// good → bad with prob a = LossRate / (BurstFactor · (1 - LossRate))
		a := l.LossRate / (float64(l.BurstFactor) * (1 - l.LossRate))
		if rng.Float64() < a {
			ls.state = 1
		}
	} else {
		// bad → good with prob 1/BurstFactor.
		if rng.Float64() < 1.0/float64(l.BurstFactor) {
			ls.state = 0
		}
	}

	if ls.state == 1 {
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
// Stateful: per-pair state map. Per-sim freshness is the contract —
// construct via NewCorrelatedLinkDelay inside Scenario.Apply so each
// iteration gets its own instance; the DES (single-goroutine within
// one sim) is the only caller of Delay, so no mutex is needed. Sharing
// across sims would break determinism AND cross-contaminate state.
type CorrelatedLinkDelay struct {
	Inner             NetworkModel
	BadLinkProb       float64
	BadLinkMultiplier float64
	BurstMessages     int

	linkBad  map[linkKey]bool
	linkSeen map[linkKey]bool
}

// linkKey identifies a pair of operators in undirected form: the two
// IDs are sorted at construction so (A→B) and (B→A) share state. The
// model is documented as "per-pair sustained flakiness" — a degraded
// physical path slows both directions simultaneously — so the chain
// for a link must not split across the two directions. Use newLinkKey
// to construct.
type linkKey struct {
	lo, hi OperatorID
}

func newLinkKey(a, b OperatorID) linkKey {
	if a > b {
		a, b = b, a
	}
	return linkKey{lo: a, hi: b}
}

// NewCorrelatedLinkDelay constructs a CorrelatedLinkDelay with fresh
// per-pair state. Use one per sim to preserve determinism.
//
// burstMessages < 1 is normalized to 1 (geometric memoryless flakiness)
// at construction time so the struct's published field matches the
// effective value.
func NewCorrelatedLinkDelay(inner NetworkModel, badLinkProb, badLinkMultiplier float64, burstMessages int) *CorrelatedLinkDelay {
	if burstMessages < 1 {
		burstMessages = 1
	}
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
	pair := newLinkKey(from, to)

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
		// good → bad with prob a. Guard BadLinkProb >= 1 first so we
		// never divide by (1 - BadLinkProb) when it's zero / negative
		// (would silently produce +Inf or NaN before falling back to 1).
		// At BadLinkProb=1 this branch IS reachable in practice: init
		// always sets linkBad=true on call #1, but the bad → good
		// branch below can flip it back via the 1/BurstMessages
		// recovery roll, and subsequent calls then re-enter this good
		// branch — where a=1 immediately snaps the link back to bad.
		// The guard's job is just to keep the algebra defined at the
		// degenerate end-point.
		var a float64
		if c.BadLinkProb >= 1 {
			a = 1
		} else {
			a = c.BadLinkProb / (float64(c.BurstMessages) * (1 - c.BadLinkProb))
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

	base := c.Inner.Delay(rng, from, to, kind)
	if bad {
		return time.Duration(float64(base) * c.BadLinkMultiplier)
	}
	return base
}
