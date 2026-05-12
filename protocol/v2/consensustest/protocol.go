// Package consensustest is a virtual-time discrete-event test framework
// for SSV consensus protocols. It defines an algorithm-agnostic Protocol
// interface so the same scenarios run through OBFT, QBFT, and future
// protocol-family members on apples-to-apples footing.
//
// Per-protocol adapters live under consensustest/{name}/ and translate the
// abstract ByzPattern into the protocol's internal byz model.
//
// Universal safety invariants — Agreement (SingleV / HonestAgreement /
// NoOfflineDoubleV), QuorumBackedDecision, NoEquivocationAccepted, plus
// OBFT-specific OBFTCommitKindValid (every commit is σ-or-NR-backed) and
// OBFTHostValidityRespect (decided value satisfies every honest
// validator's host-validity predicate) — are enforced on every simulation
// regardless of profile. Violations panic via SafetyPanic.
//
// Invariants checked from PerOp/OfflineAgg directly (Agreement); the rest
// read Outcome.CommitAttestation, populated by adapters. Adapters that
// haven't instrumented a given invariant leave the corresponding *Checked
// field zero; the framework treats unchecked invariants as
// no-violation-reportable (graceful degradation, same pattern as
// NoOfflineDoubleV pre-instrumentation).
package consensustest

import (
	"fmt"
	"time"
)

// OperatorID identifies a participant. IDs are 1..N; adapters convert as
// needed for their internal types.
type OperatorID uint64

// SimConfig is the algorithm-agnostic input to one simulation. Per-protocol
// adapters translate fields into their internal config. Timing is anchored at
// slot start (virtual-time 0); RelayCutoff is the hard deadline by which a
// signed output must be produced.
//
// Configurable spec parameters are first-class fields; everything else is
// derived inside adapters per OBFT.md §Setting / §Application:
//   - T_commit         = RelayCutoff − HeaderSubmitHeadroom − Phase3JitterBuffer − Epsilon3 − Delta2
//   - T_broadcast_max  = T_commit − BroadcastBudget[k]   (T_commit-anchored)
//   - qV = qEnc        = 2f+1 from N
//   - F                = (N−1)/3
type SimConfig struct {
	N         int          // cluster size; F = (N-1)/3 is implied
	Operators []OperatorID // typically 1..N

	// K is the OBFT layer count. Defaults via DefaultK(N) to N (every operator
	// leads exactly one layer — SSV production convention). QBFT adapters
	// ignore this. Must satisfy MinK(N) ≤ K ≤ N where MinK = f+1.
	K int

	SlotStart    time.Duration // virtual-time offset of slot start; usually 0
	SlotDuration time.Duration // 12s for Ethereum
	RelayCutoff  time.Duration // application hard deadline (4s for proposer duty)

	// HeaderSubmitHeadroom is reserved between consensus completion and
	// RelayCutoff for cert broadcast + relay submit. 100ms at the operating point.
	HeaderSubmitHeadroom time.Duration

	// BTT (broadcast trip time) = network P99 one-way propagation + clock skew δ.
	// Protocols derive their per-phase windows from this (OBFT Δ_2 = 2 BTT;
	// QBFT round trip ≈ 1 BTT per message).
	BTT time.Duration

	// Delta2 is the OBFT Phase-2 propagation budget. Defaults to 2*BTT if zero.
	Delta2 time.Duration

	// Epsilon3 is the OBFT Phase-3 local-CPU budget (BLS aggregation + IBE
	// decryption walk + certificate construction). Per OBFT.md §Phase 3 /
	// §Timing budget: `Δ_3 ≈ ε_3 ≈ 50ms` at Config A. Absolute (does not scale
	// with BTT). Defaults to 50ms if zero. Passed verbatim to obft.Config.Delta3
	// (the production single-instance ε_3 field).
	Epsilon3 time.Duration

	// Phase3JitterBuffer is the residual jitter buffer between Phase-3
	// completion and cert/submit. Per OBFT.md §Timing budget: "the 600ms after
	// T_commit decomposes as Δ_2 (400ms) + Δ_3 (50ms) + header_submit_headroom
	// (100ms) + ~50ms residual jitter buffer". Defaults to 50ms. Used only in
	// the T_commit anchor derivation; production obft.Config does not consume
	// this directly.
	Phase3JitterBuffer time.Duration

	// BroadcastBudget carries the OBFT per-layer T_commit-anchored propagation
	// budget B_k (per spec §Setting). Strictly increasing in k. When nil,
	// derived from BTT + T_commit via DefaultBkSchedule(K, BTT, T_commit).
	BroadcastBudget []time.Duration

	// FetchAt carries the OBFT per-layer leader fetch offsets. Strictly
	// decreasing in k (deeper layers fetch from deeper-confirmed parents).
	// When nil, derived from BTT via DefaultFetchSchedule(K, BTT, T_commit,
	// LeaderBroadcastOffset).
	FetchAt []time.Duration

	// LeaderBroadcastOffset overrides the per-layer fetch buffer (BTT/4 default)
	// used when FetchAt is derived from defaults. Map key is the layer index;
	// missing key falls back to the default buffer. A zero offset places the
	// leader's broadcast exactly at T_broadcast_max_k — the spec's max-MEV
	// operating point per OBFT.md §Timing budget. Use WithMaxMEVFetch() for the
	// every-leader-at-the-boundary convenience setup.
	//
	// Ignored when FetchAt is set explicitly.
	LeaderBroadcastOffset map[int]time.Duration

	// QBFTRoundTimeout is the QBFT per-round timer (RT). Defaults to 2s.
	// Used by qbft.Protocol variants with UseFixedRT=true (QBFT-SSV);
	// QBFT (default) computes RT from PhaseBudget instead.
	QBFTRoundTimeout time.Duration

	// PhaseBudget is the QBFT per-phase time budget (PROPOSE, PREPARE,
	// COMMIT, ROUND_CHANGE, post-consensus margin). Defaults to 2·BTT
	// to mirror OBFT's Δ_2 = 2·BTT propagation convention. Used by:
	//   - qbft.Protocol (computed-RT variant): RT = 3 × PhaseBudget.
	//   - qbft DES events.go: post-consensus margin = PhaseBudget.
	// QBFT-SSV (UseFixedRT=true) consumes PhaseBudget only for the
	// post-consensus margin; its RT is QBFTRoundTimeout.
	PhaseBudget time.Duration

	Network NetworkModel
	Host    HostPattern

	// Byz translates to the protocol's internal byz model; kinds an adapter
	// can't faithfully translate cause it to return ErrNotApplicable.
	Byz ByzPattern

	Seed int64 // (Seed, SimConfig) → byte-identical event trace

	// TraceEnabled records every dispatched event into Outcome.Trace; default
	// off (turn on to replay an assertion failure).
	TraceEnabled bool

	// BLSKeys, when non-nil, switches the sim to real BLS for adapters that
	// support it. Generate with GenerateBLSKeys; reuse across sims.
	BLSKeys *BLSKeys
}

// F returns the byzantine bound implied by N (F = (N-1)/3).
func (c *SimConfig) F() int {
	return (c.N - 1) / 3
}

// DefaultK returns SSV's recommended K for cluster size n: K = n. Every
// operator leads exactly one layer per slot. Mirrors production obft.DefaultK.
func DefaultK(n int) int { return n }

// MinK returns the BFT-liveness K floor for cluster size n: f+1 where
// f = (n-1)/3. Pigeonhole over the f-byz bound guarantees ≥ 1 honest
// leader. At K < f+1 all leaders could be byzantine and no σ-quorum
// reaches at any layer.
//
// Policy note: OBFT.md §Setting also describes K ≥ f+2 as providing
// late-leader-resilience (≥ 2 honest leaders); that choice is left to
// the operator/deployment per spec and is NOT enforced here. The
// framework accepts the full spec range `f+1 ≤ K ≤ n`.
func MinK(n int) int {
	f := (n - 1) / 3
	return f + 1
}

// Protocol is implemented by per-algorithm adapters. Adapters MUST be
// deterministic given (cfg, cfg.Seed) — two calls with the same input must
// produce byte-identical Outcome.Trace.
type Protocol interface {
	Name() string
	Run(cfg SimConfig) (Outcome, error)
}

// ErrNotApplicable is returned by Run when a scenario doesn't translate to
// this protocol (e.g. OBFT-specific h_V=1 on QBFT). The framework treats it
// as "skip" rather than "fail" when comparing outcomes (renders as n/a).
var ErrNotApplicable = fmt.Errorf("scenario not applicable to this protocol")

// ErrConfigOutOfEnvelope signals that the SimConfig itself is infeasible at
// the requested operating point — the protocol applies in principle, but the
// derived schedule (B_k, FetchAt, T_commit) can't be made strict-monotonic /
// non-negative. The framework treats it as a protocol failure (renders as
// 0% / red) rather than n/a, since the protocol genuinely cannot operate at
// that configuration.
var ErrConfigOutOfEnvelope = fmt.Errorf("config out of envelope for this protocol")

// Outcome is the algorithm-agnostic per-sim result.
type Outcome struct {
	Decided      bool
	DecisionTime time.Duration // earliest cluster-wide successful decision; 0 if !Decided
	DecidedValue []byte        // protocol-opaque
	// DecidedRound is 0-indexed (OBFT layer or QBFT round-1); -1 if !Decided.
	DecidedRound int
	PerOp        map[OperatorID]OperatorOutcome
	Trace        []TraceEntry // non-nil iff cfg.TraceEnabled was set

	// Bandwidth aggregates per-message byte counts emitted during the sim.
	// Populated by adapters that instrument message emission.
	Bandwidth BandwidthReport

	// OfflineAgg is the post-sim offline-aggregator's reconstruction attempt.
	// A safety violation (NoOfflineDoubleV=false) panics in the runner.
	OfflineAgg OfflineAggReport

	// CommitAttestation aggregates adapter-side observations the framework
	// uses to verify safety invariants beyond Agreement (which it checks
	// directly from PerOp). Adapters that haven't instrumented a given
	// invariant leave the corresponding *Checked field zero — the framework
	// treats unchecked invariants as no-violation-reportable.
	CommitAttestation CommitAttestation
}

// CommitAttestation carries adapter-introspected evidence for the
// per-decision safety invariants. Each invariant has a *Checked bool the
// adapter sets when it ran the instrumentation, plus diagnostic fields
// the framework reads to decide OK vs violation. Default zero values =
// "adapter didn't instrument; no violation reportable".
//
// Adapter migration: an adapter starts with all *Checked=false and is
// gradually instrumented. The framework's safety check still passes for
// uninstrumented adapters, so adding new invariants is non-breaking.
type CommitAttestation struct {
	// QuorumBackedDecision: when set, the decided value must be backed by
	// QuorumSigners >= QuorumRequired (typically 2f+1). Adapter sets when
	// the decision carried a verifiable quorum certificate.
	QuorumChecked  bool
	QuorumSigners  int
	QuorumRequired int

	// NoEquivocationAccepted: when set, the adapter checked that no
	// honest validator committed based on a proposal whose leader had
	// equivocated within the same (instance, round). EquivocationsObserved
	// is diagnostic (>0 means the scenario actually generated
	// equivocations — distinguishes vacuous-pass from tested-pass).
	// EquivocationsAccepted > 0 is a violation.
	EquivocationChecked   bool
	EquivocationsObserved int
	EquivocationsAccepted int

	// OBFTCommitKindValid (OBFT-specific): when set, the adapter recorded
	// the certificate kind that justified the commit. Valid kinds are
	// "sigma" and "nr"; any other value (including empty when checked) is
	// a violation.
	OBFTCommitKindChecked bool
	OBFTCommitKind        string

	// OBFTHostValidityRespect (OBFT-specific): when set, the adapter
	// compared the decided value against each honest validator's
	// host-validity predicate. OBFTHostValidityRejecters > 0 is a
	// violation.
	OBFTHostValidityChecked   bool
	OBFTHostValidityRejecters int
}

type OperatorOutcome struct {
	Decided bool
	Value   []byte
	Round   int // round / layer
	Time    time.Duration
	Err     string

	// EvidenceByRule maps protocol-specific rule names to fire counts for
	// this operator. Rule names follow the convention "<Protocol>/<RuleName>"
	// (e.g. "OBFT/Rule4", "OBFT/Rule5/cryptoFake", "QBFT/equivocation").
	// A rule name absent from the map means zero fires.
	EvidenceByRule map[string]int

	// BandwidthOut / BandwidthIn are byte counts emitted / received by this
	// operator. Adapters that don't instrument bandwidth leave both at zero.
	BandwidthOut int64
	BandwidthIn  int64
}

// EvidenceCount is a convenience: total fires across all rules for this op.
// Returns zero on nil EvidenceByRule.
func (o OperatorOutcome) EvidenceCount() int {
	total := 0
	for _, n := range o.EvidenceByRule {
		total += n
	}
	return total
}

type TraceEntry struct {
	When  time.Duration
	Event string
}

// BLSKeys is a threshold-shared BLS keypair. Adapters that support real BLS
// read this via SimConfig.BLSKeys.
type BLSKeys struct {
	ClusterPubKey []byte
	Shares        map[OperatorID][]byte // herumi-format secret shares
	PubShares     map[OperatorID][]byte // herumi-format public shares
}

// Validate sanity-checks the config and fills defaults.
func (c *SimConfig) Validate() error {
	if c.N < 4 {
		return fmt.Errorf("consensustest: N must be >= 4 (n = 3f+1 minimum)")
	}
	if (c.N-1)%3 != 0 {
		return fmt.Errorf("consensustest: N must be 3f+1 (got %d)", c.N)
	}
	if len(c.Operators) != c.N {
		return fmt.Errorf("consensustest: Operators length %d != N %d", len(c.Operators), c.N)
	}
	seenOps := make(map[OperatorID]struct{}, c.N)
	for _, op := range c.Operators {
		if _, dup := seenOps[op]; dup {
			return fmt.Errorf("consensustest: duplicate operator ID %d in Operators", op)
		}
		seenOps[op] = struct{}{}
	}
	if c.BTT <= 0 {
		return fmt.Errorf("consensustest: BTT must be > 0")
	}
	if c.RelayCutoff <= 0 {
		return fmt.Errorf("consensustest: RelayCutoff must be > 0")
	}
	if c.SlotDuration <= 0 {
		return fmt.Errorf("consensustest: SlotDuration must be > 0")
	}
	if c.RelayCutoff > c.SlotDuration {
		return fmt.Errorf("consensustest: RelayCutoff (%v) > SlotDuration (%v)", c.RelayCutoff, c.SlotDuration)
	}

	if c.K == 0 {
		c.K = DefaultK(c.N)
	}
	minK := MinK(c.N)
	if c.K < minK {
		return fmt.Errorf("consensustest: K=%d below BFT-liveness minimum %d (= f+1 at n=%d)",
			c.K, minK, c.N)
	}
	if c.K > c.N {
		return fmt.Errorf("consensustest: K=%d exceeds N=%d", c.K, c.N)
	}

	if c.HeaderSubmitHeadroom == 0 {
		c.HeaderSubmitHeadroom = 100 * time.Millisecond
	}
	if c.Delta2 == 0 {
		c.Delta2 = 2 * c.BTT
	}
	if c.Epsilon3 == 0 {
		c.Epsilon3 = 50 * time.Millisecond
	}
	if c.Phase3JitterBuffer == 0 {
		c.Phase3JitterBuffer = 50 * time.Millisecond
	}
	if c.QBFTRoundTimeout == 0 {
		c.QBFTRoundTimeout = 2 * time.Second
	}
	if c.PhaseBudget == 0 {
		c.PhaseBudget = 2 * c.BTT
	}

	tCommit := c.RelayCutoff - c.HeaderSubmitHeadroom - c.Phase3JitterBuffer - c.Epsilon3 - c.Delta2
	if tCommit <= 0 {
		return fmt.Errorf("consensustest: derived T_commit=%v is non-positive (RelayCutoff=%v HeaderSubmit=%v Phase3JitterBuffer=%v Epsilon3=%v Delta2=%v)",
			tCommit, c.RelayCutoff, c.HeaderSubmitHeadroom, c.Phase3JitterBuffer, c.Epsilon3, c.Delta2)
	}

	if c.BroadcastBudget == nil {
		bk, err := DefaultBkSchedule(c.K, c.BTT, tCommit)
		if err != nil {
			return fmt.Errorf("consensustest: %w", err)
		}
		c.BroadcastBudget = bk
	}
	if len(c.BroadcastBudget) != c.K {
		return fmt.Errorf("consensustest: BroadcastBudget has %d entries, expected K=%d",
			len(c.BroadcastBudget), c.K)
	}
	for k := 1; k < c.K; k++ {
		if c.BroadcastBudget[k] <= c.BroadcastBudget[k-1] {
			return fmt.Errorf("consensustest: BroadcastBudget must be strictly increasing in k (B_%d=%v <= B_%d=%v)",
				k, c.BroadcastBudget[k], k-1, c.BroadcastBudget[k-1])
		}
	}

	if c.FetchAt == nil {
		fa, err := DefaultFetchSchedule(c.K, c.BTT, tCommit, c.LeaderBroadcastOffset)
		if err != nil {
			return fmt.Errorf("consensustest: %w", err)
		}
		c.FetchAt = fa
	}
	if len(c.FetchAt) != c.K {
		return fmt.Errorf("consensustest: FetchAt has %d entries, expected K=%d",
			len(c.FetchAt), c.K)
	}
	for k := 1; k < c.K; k++ {
		if c.FetchAt[k] >= c.FetchAt[k-1] {
			return fmt.Errorf("consensustest: FetchAt must be strictly decreasing in k (T_%d=%v >= T_%d=%v)",
				k, c.FetchAt[k], k-1, c.FetchAt[k-1])
		}
	}

	f := c.F()
	seenByz := make(map[OperatorID]struct{}, len(c.Byz.ByzOperators))
	for _, op := range c.Byz.ByzOperators {
		if _, dup := seenByz[op]; dup {
			return fmt.Errorf("consensustest: ByzPattern.ByzOperators has duplicate operator %d", op)
		}
		seenByz[op] = struct{}{}
	}
	if len(c.Byz.ByzOperators) > f {
		return fmt.Errorf("consensustest: ByzPattern has %d byz operators but f=%d (cluster N=%d)",
			len(c.Byz.ByzOperators), f, c.N)
	}
	seenRecipients := make(map[OperatorID]struct{}, len(c.Byz.Recipients))
	for _, op := range c.Byz.Recipients {
		if _, dup := seenRecipients[op]; dup {
			return fmt.Errorf("consensustest: ByzPattern.Recipients has duplicate operator %d (positional convention requires distinct recipients)", op)
		}
		seenRecipients[op] = struct{}{}
	}

	if c.Network == nil {
		c.Network = ConstantDelay{D: c.BTT}
	}
	if c.Host == nil {
		c.Host = HostAllValid{}
	}
	return nil
}

// DefaultProposerDutyConfig returns a SimConfig at OBFT.md §Application's
// recommended proposer-duty operating point: n=4, RelayCutoff=4s. Pass `btt`
// to scale the operating point (200ms is the spec target; smaller stresses
// ideal mesh, larger stresses degraded mesh).
func DefaultProposerDutyConfig(btt time.Duration) SimConfig {
	operators := make([]OperatorID, 4)
	for i := range operators {
		operators[i] = OperatorID(i + 1)
	}
	return SimConfig{
		N:                    4,
		Operators:            operators,
		K:                    0, // → DefaultK(N=4) = 4 (K = N convention)
		SlotStart:            0,
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		Network:              ConstantDelay{D: btt},
		Host:                 HostAllValid{},
		Byz:                  ByzPattern{Kind: ByzNone},
		Seed:                 1,
	}
}

// WithMaxMEVFetch sets per-layer LeaderBroadcastOffset to zero for every layer
// in [0, K), placing each leader's broadcast exactly at T_broadcast_max_k.
// This is the spec's max-MEV boundary operating point (OBFT.md §Timing
// budget) — the freshest possible fetch window per layer at the cost of zero
// propagation safety margin within that layer's budget. Call after K is set
// (or defaults are applied via Validate).
//
// Ignored if c.FetchAt is set explicitly (FetchAt takes precedence over
// derived schedules).
func (c *SimConfig) WithMaxMEVFetch() {
	k := c.K
	if k == 0 {
		k = DefaultK(c.N)
	}
	c.LeaderBroadcastOffset = make(map[int]time.Duration, k)
	for i := 0; i < k; i++ {
		c.LeaderBroadcastOffset[i] = 0
	}
}

// ClusterSizes are the SSV-supported cluster sizes (n = 3f+1 for f ∈ {1..4}).
var ClusterSizes = []int{4, 7, 10, 13}

// MakeOperators returns Operators 1..n.
func MakeOperators(n int) []OperatorID {
	ops := make([]OperatorID, n)
	for i := range ops {
		ops[i] = OperatorID(i + 1)
	}
	return ops
}
