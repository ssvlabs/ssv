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
// validator's host-validity predicate) — are the advertised safety set.
// The framework runs IsViolation over every simulation regardless of
// profile and panics on a violation via SafetyPanic.
//
// Effective coverage today is narrower than that list. Only the Agreement
// trio is load-bearing: it is computed directly from PerOp/OfflineAgg and
// always fires. The other four read Outcome.CommitAttestation and cannot
// fire under the current adapters — three (QuorumBackedDecision,
// OBFTCommitKindValid, OBFTHostValidityRespect) have their *Checked gate set
// by no adapter, and NoEquivocationAccepted is gated on but its trigger
// (EquivocationsAccepted) is hardwired to 0. They default to true and are
// retained so a future adapter can opt in without a framework-side change
// (graceful degradation, same pattern as NoOfflineDoubleV pre-instrumentation).
// See SafetyReport in safety.go for the per-invariant coverage table.
package consensustest

import (
	"fmt"
	mrand "math/rand"
	"slices"
	"strings"
	"time"
)

// OperatorID identifies a participant. IDs are 1..N; adapters convert as
// needed for their internal types.
type OperatorID uint64

// SimConfig is the algorithm-agnostic input to one simulation. Per-protocol
// adapters translate fields into their internal config. Timing is anchored
// at slot start (virtual-time 0); RelayCutoff is the hard deadline by which
// a signed output must be produced.
//
// SimConfig carries only what the framework or MULTIPLE adapters need to
// share. Protocol-specific timing derivation (OBFT's Δ_2 / B_k schedule /
// FetchAt; QBFT's per-round RT) lives in the per-adapter Run path and is
// derived from BTT at Run-time. The bridge value is BTT — some adapters
// scale it by a per-variant BTTMultiplier (e.g. OBFT's x2/x3) before deriving
// budgets, so the sweep can drive every protocol family from the same
// network BTT while letting each variant model a different operator-side
// pessimism.
//
//   - qV = qEnc       = 2f+1 from N
//   - F               = (N−1)/3
//   - T_commit, B_k, FetchAt, Delta2, RT, PhaseBudget — all derived
//     inside the relevant adapter from BTT (× the variant's multiplier)
//     and RelayCutoff − HeaderSubmitHeadroom.
type SimConfig struct {
	N         int          // cluster size; F = (N-1)/3 is implied
	Operators []OperatorID // typically 1..N

	// K is the OBFT layer count. Defaults via DefaultK(N) to N (every operator
	// leads exactly one layer — SSV production convention). QBFT adapters
	// ignore this. Must satisfy MinK(N) ≤ K ≤ N where MinK = f+1.
	K int

	// BFTStart is the virtual-time offset at which the protocol's primary
	// broadcast pipeline begins (= BFT_start in the spec). Pre-fetch and
	// pre-consensus modeling are not in scope for the sim; this field
	// captures BFT activity start directly. Defaults to 0 (BFT starts at
	// slot start). The OBFT-family schedules apply the spec's runtime
	// clamp `T_broadcast_max_k = max(BFTStart, T_commit − B_k)`; PSigs
	// uses this as the time each op signs + broadcasts; QBFT uses it as
	// the PROPOSE-start anchor.
	BFTStart     time.Duration
	SlotDuration time.Duration // 12s for Ethereum
	RelayCutoff  time.Duration // application hard deadline (4s for proposer duty)

	// HeaderSubmitHeadroom is reserved between consensus completion and
	// RelayCutoff for cert broadcast + relay submit. Operator-side
	// (cert-build + I/O cost), not network propagation — every adapter
	// reads it for ClipLateDecision against `RelayCutoff −
	// HeaderSubmitHeadroom`. 100ms at the spec's operating point.
	HeaderSubmitHeadroom time.Duration

	// BTT (broadcast trip time) = network P99 one-way propagation + clock skew δ.
	// The single network-side knob the sweep varies; each Protocol-family
	// adapter scales BTT by its own BTTMultiplier (default 1.0) before
	// deriving every internal timing budget. Framework-side sizing
	// (spec-aligned, post-tightening): OBFT Δ_2 = 1·bttEff (spec
	// recommendation; reflood absorbed by B_0, not by Δ_2 — see
	// RefloodDelay); 2abOBFT Δ_2a + Δ_2b = 3·bttEff total (Δ_2a =
	// 2·bttEff structural minimum per 2abOBFT.md §Setting, Δ_2b =
	// 1·bttEff spec-aligned); QBFT pristine per-round RT = 3·bttEff (R1)
	// / 4·bttEff (rounds ≥ 2, +1·BTT ROUND_CHANGE hop), QBFTSSV flat 2s.
	BTT time.Duration

	// RefloodDelay is the per-scenario reflood-absorption budget folded
	// into the OBFT-family primary-layer broadcast schedule (`B_0 = 2·BTT
	// + RefloodDelay`; backups unaffected). Default 0: adversarial scenarios
	// and direct-delivery sims model idealized eager-push and don't pay
	// schedule-level reflood absorption. Production-realistic-mesh
	// scenarios opt in by setting cfg.RefloodDelay equal to the mesh's
	// gossip heartbeat (700ms by default), matching the production SSV
	// adapter's DefaultRefloodDelay. Does NOT scale with BTTMultiplier —
	// reflood-cycle latency is a libp2p deployment constant (mirrors
	// QBFT-SSV's fixed 2s round-timeout convention). Ignored by QBFT and PSigs adapters
	// (no spec-level reflood-absorption concept).
	RefloodDelay time.Duration

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

	// Delivery selects the transport model. Default DeliveryDirect (full
	// fanout, current behavior). DeliveryMesh routes every emit through
	// a small-world mesh with dedup + reflood — opt-in per scenario via
	// Scenario.Delivery, propagated here by Scenario.Apply or by the
	// framework's Run path when scenarios trigger.
	Delivery DeliveryMode

	// Mesh is the per-sim mesh tunable bundle, consulted only when
	// Delivery == DeliveryMesh. Adapters call MakeMeshTopology(cfg) at
	// sim start to materialize the topology; an unset HopDelay there
	// panics, surfacing miswired delivery configs at sim build rather
	// than as a silent zero-delay mesh.
	Mesh MeshConfig
}

// MakeMeshTopology constructs the per-sim MeshTopology for `c` if
// Delivery == DeliveryMesh, returning nil otherwise. Adapters call this
// once per sim (inside their Run) and store the result on their sim
// state; the topology lives for the sim's lifetime. The returned
// MeshTopology owns its dedup state — never share across sims.
func (c *SimConfig) MakeMeshTopology() *MeshTopology {
	if c.Delivery != DeliveryMesh {
		return nil
	}
	// Crashed operators are fully offline — they don't participate in the
	// mesh at all (neither originate nor forward), so the topology is built
	// on the surviving cluster peers only. Survivors thus lose the crashed
	// nodes' relay paths, which is the realistic "node down" propagation
	// effect (vs. a half-present ghost that still forwards). Crashed is
	// already resolved + bounded by Validate before any adapter calls this.
	cluster := operatorsExcluding(c.Operators, c.Byz.Crashed)
	return NewMeshTopology(c.Seed, c.Mesh, cluster)
}

// operatorsExcluding returns ops with every member of exclude removed,
// preserving order. Returns ops unchanged when exclude is empty.
func operatorsExcluding(ops, exclude []OperatorID) []OperatorID {
	if len(exclude) == 0 {
		return ops
	}
	ex := make(map[OperatorID]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	out := make([]OperatorID, 0, len(ops))
	for _, op := range ops {
		if !ex[op] {
			out = append(out, op)
		}
	}
	return out
}

// selectCrashed deterministically picks `count` distinct operators from
// `ops` (excluding any in `exclude`) using `seed`. The seed dependence lets
// the Healthy faulty_nodes knob average over victim positions across iters
// while staying reproducible — every protocol resolves the same crashed set
// at a given (scenario, seed). The salt decorrelates this draw from the
// network-model / mesh RNG streams (which seed off the same value). Result
// is sorted for stable downstream iteration.
func selectCrashed(seed int64, ops []OperatorID, exclude []OperatorID, count int) []OperatorID {
	if count <= 0 {
		return nil
	}
	// Copy before shuffling: operatorsExcluding may alias `ops` when
	// `exclude` is empty, and an in-place shuffle would corrupt the
	// caller's Operators ordering (load-bearing for leader rotation).
	pool := append([]OperatorID(nil), operatorsExcluding(ops, exclude)...)
	rng := mrand.New(mrand.NewSource(seed ^ 0x6661756c74795f76)) // "faulty_v"
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if count > len(pool) {
		count = len(pool)
	}
	chosen := append([]OperatorID(nil), pool[:count]...)
	slices.Sort(chosen)
	return chosen
}

// F returns the byzantine bound implied by N (F = (N-1)/3).
func (c *SimConfig) F() int {
	return (c.N - 1) / 3
}

// DefaultK returns SSV's recommended K for cluster size n: K = f+1 (the
// BFT-liveness minimum, where f = (n-1)/3). At n=4 (f=1) this is K=2; at
// larger n it scales (n=7 → K=3, n=10 → K=4, n=13 → K=5). Mirrors
// production obft.DefaultK + setup_obft.go's K = max(DefaultK, f+1) rule.
// Motivated by spec docs/OBFT.md §K-tuning: production testing showed
// K > f+1 doesn't materially improve outcomes once peer-reflood-V closes
// the h_V=1 case at L_0.
func DefaultK(n int) int { return (n-1)/3 + 1 }

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

// IsPipelineShiftProtocol reports whether the UI uses post-hoc pipeline-shift
// (decision_time + BFTStart) for this protocol — i.e. the protocol's whole
// pipeline shifts wholesale with BFTStart and one BFTStart=0 sim is
// equivalent to running at any BFTStart. Returns true for QBFT family and
// PSigs; false for OBFT family (where per-layer broadcast schedules are
// slot-anchored and BFTStart shifts only the lower bound of each layer's
// broadcast deadline — requires a real per-BFTStart simulation, not a shift).
//
// Drives sweep-matrix protocol filtering: at BFTStart > 0 the sweep skips
// pipeline-shift protocols (the BFTStart=0 cell + UI shift already covers
// them) and only runs the OBFT-family ones for which BFTStart materially
// changes the schedule.
func IsPipelineShiftProtocol(p Protocol) bool {
	name := p.Name()
	return name == "QBFT" || name == "PSigs" || strings.HasPrefix(name, "QBFT-")
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

// ClipLateDecision converts a successful Outcome whose earliest-decider time
// exceeds `deadline` into a MISS, and clears every per-op decided=true that
// also misses the deadline. Adapters apply this in their Run after the DES
// completes so the framework's notion of "decided" matches production: a
// cluster that doesn't have the cert / decision in hand by RelayCutoff −
// HeaderSubmitHeadroom can't submit, regardless of how the protocol got
// there internally (QBFT round-changing past its timer, OBFT recovering via
// evtResolveRerun on a late KindCommit, etc.). Earliest-decider gate matches
// the framework's "any in-time op submits and the slot succeeds" semantic.
//
// Post-clip Outcome fields:
//   - Decided               = false
//   - DecidedRound          = -1
//   - DecidingBroadcastTime = 0 (enforces the "Zero when !Decided" contract
//     documented on Outcome.DecidingBroadcastTime; OBFT-family adapters
//     stamp this BEFORE calling ClipLateDecision, so the zero-out here
//     prevents the per-iter contract violation on clipped iters)
//   - DecidedValue          = preserved (diagnostic: which V the protocol
//     would have decided had it landed in time)
//   - DecisionTime          = preserved (diagnostic: when the late decision
//     actually landed; useful for measuring "how far past the deadline?")
//   - PerOp[op].Time        = preserved on the same basis as DecisionTime
//
// Aggregation only reads DecisionTime / DecidedValue / DecidingBroadcastTime
// when r.out.Decided is true (see aggregateCellIters), so the preserved
// values don't leak into reported metrics — they're available solely for
// trace diagnosis.
func ClipLateDecision(out *Outcome, deadline time.Duration) {
	if !out.Decided || out.DecisionTime <= deadline {
		return
	}
	out.Decided = false
	out.DecidedRound = -1
	out.DecidingBroadcastTime = 0
	for op, oo := range out.PerOp {
		if oo.Decided && oo.Time > deadline {
			oo.Decided = false
			oo.Round = -1
			if oo.Err == "" {
				oo.Err = "missed relay deadline"
			}
			out.PerOp[op] = oo
		}
	}
}

// Outcome is the algorithm-agnostic per-sim result.
type Outcome struct {
	Decided      bool
	DecisionTime time.Duration // earliest cluster-wide successful decision; 0 if !Decided
	DecidedValue []byte        // protocol-opaque
	// DecidedRound is 0-indexed (OBFT layer or QBFT round-1); -1 if !Decided.
	DecidedRound int
	// MissReason is the adapter-set human-readable label for !Decided
	// outcomes. Populated post-ClipLateDecision by each adapter using
	// protocol-specific context (pre-clip round / layer, byz pattern,
	// etc.) so the report can render rich categorization like
	// "Cluster ready to submit at layer 2, past the submit deadline"
	// or "Cluster never assembled a threshold signature at any layer".
	// Labels are intentionally NOT parameterised on per-iter lateness
	// in ms — the DecisionTime distribution carries that signal;
	// grouping the failure-breakdown table by category keeps the row
	// count bounded (≤ K rows for the "decided late" family per
	// protocol). Setting MissReason on !Decided is part of the adapter
	// contract; the framework's classifyMiss buckets a missing label
	// under "Unknown failure (adapter did not set MissReason)" as a
	// safety net. Empty when Decided=true.
	MissReason string
	// DecidingBroadcastTime is the slot-anchored T_broadcast_max_k for
	// k=DecidedRound — i.e. the absolute slot time at which the deciding
	// layer's primary broadcast fired (= max(BFTStart, T_commit − B_k)
	// per the spec's runtime clamp). Reported for diagnostic / UI use
	// and per-sample timeline rendering.
	//
	// Set only by adapters with a slot-anchored Phase-1 schedule (OBFT
	// family and 2abOBFT family). QBFT's pipeline shifts wholesale with
	// BFTStart so the field is meaningless there; the QBFT adapter
	// leaves it zero and the UI's shiftCell pipeline-shift branch
	// doesn't consult it. Zero when !Decided.
	DecidingBroadcastTime time.Duration
	// BFTStartIndependenceThreshold is the largest BFTStart for which the
	// deciding-layer (L_0) primary broadcast schedule is bit-identical to
	// BFTStart=0 — i.e. the *unclamped* fetchAt[0] (= anchor − B_0,
	// floored at 0, where anchor = T_commit for OBFT / T0Broadcast for
	// 2abOBFT and B_0 = broadcastBudget[0]). At or below this value the
	// spec clamp `max(BFTStart, anchor − B_0)` is dormant at L_0, so the
	// BFTStart=0 cell is an exact stand-in; above it a per-BFTStart
	// pre-computed cell is required. The UI reads the serialized value to
	// decide cell reuse instead of recomputing the sizing JS-side (which
	// drifts whenever the adapter's timing changes).
	//
	// A pointer so that an emitted 0 (clamp engages immediately) is
	// distinct from "not set". Set only by adapters with a slot-anchored
	// Phase-1 schedule (OBFT family and 2abOBFT family), and only on the
	// BFTStart=0 cell: the value is BFTStart-invariant and the UI reads it
	// from that anchor cell, so one copy suffices (BFTStart>0 cells leave
	// it nil). nil for pipeline-shift protocols (QBFT family, PSigs),
	// which shift wholesale and whose UI path never consults it. Unlike
	// DecidingBroadcastTime it's a pure function of cfg (not the sim
	// outcome), so it survives ClipLateDecision unchanged and is valid
	// even on !Decided iters.
	BFTStartIndependenceThreshold *time.Duration
	PerOp                         map[OperatorID]OperatorOutcome
	Trace                         []TraceEntry // non-nil iff cfg.TraceEnabled was set

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
	if c.RefloodDelay < 0 {
		return fmt.Errorf("consensustest: RefloodDelay must be >= 0")
	}
	if c.BFTStart < 0 {
		return fmt.Errorf("consensustest: BFTStart must be >= 0")
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

	// Protocol-specific timing derivation (Delta2, BroadcastBudget,
	// FetchAt, RT, PhaseBudget) lives on the per-adapter Protocol struct
	// and runs inside Protocol.Run, not here. The framework checks only
	// what every adapter shares: cluster topology, slot constants, and
	// the BTT > 0 / RelayCutoff > 0 anchors above. Per-adapter T_commit
	// non-positivity (e.g. at high BTT × Δ_2 tax) surfaces as
	// ErrConfigOutOfEnvelope from the adapter, not here.

	f := c.F()
	seenByz := make(map[OperatorID]struct{}, len(c.Byz.ByzOperators))
	for _, op := range c.Byz.ByzOperators {
		if _, dup := seenByz[op]; dup {
			return fmt.Errorf("consensustest: ByzPattern.ByzOperators has duplicate operator %d", op)
		}
		seenByz[op] = struct{}{}
	}

	// Resolve a random crashed set from CrashedCount (the Healthy
	// faulty_nodes knob) when no explicit Crashed list was supplied.
	// Deterministic in Seed so every protocol crashes the same operators
	// at a given (scenario, seed); excludes ByzOperators so a crash+byz
	// combo stays within the f-budget.
	if c.Byz.CrashedCount > 0 && len(c.Byz.Crashed) == 0 {
		c.Byz.Crashed = selectCrashed(c.Seed, c.Operators, c.Byz.ByzOperators, c.Byz.CrashedCount)
	}
	opSet := make(map[OperatorID]struct{}, len(c.Operators))
	for _, op := range c.Operators {
		opSet[op] = struct{}{}
	}
	seenCrash := make(map[OperatorID]struct{}, len(c.Byz.Crashed))
	for _, op := range c.Byz.Crashed {
		if _, ok := opSet[op]; !ok {
			return fmt.Errorf("consensustest: ByzPattern.Crashed operator %d not in Operators", op)
		}
		if _, dup := seenCrash[op]; dup {
			return fmt.Errorf("consensustest: ByzPattern.Crashed has duplicate operator %d", op)
		}
		if _, isByz := seenByz[op]; isByz {
			return fmt.Errorf("consensustest: operator %d is in both ByzOperators and Crashed (faults must be disjoint)", op)
		}
		seenCrash[op] = struct{}{}
	}
	if total := len(c.Byz.ByzOperators) + len(c.Byz.Crashed); total > f {
		return fmt.Errorf("consensustest: %d byz + %d crashed = %d total faults but f=%d (cluster N=%d)",
			len(c.Byz.ByzOperators), len(c.Byz.Crashed), total, f, c.N)
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
	// Mesh HopDelay default mirrors DefaultProposerDutyConfig's anchor
	// (BTT/3 median, σ=0.3) so callers that wire Delivery=DeliveryMesh
	// onto a SimConfig built without going through DefaultProposerDutyConfig
	// — including the framework's own scenario-propagation path when a
	// scenario opts into mesh — get a working mesh transport without
	// having to remember the calibration anchor. Callers wanting a
	// different HopDelay set it explicitly before Validate runs.
	if c.Delivery == DeliveryMesh && c.Mesh.HopDelay == nil {
		c.Mesh.HopDelay = LogNormalDelay{Median: c.BTT / 3, Sigma: 0.3}
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
		K:                    0, // → DefaultK(N=4) = 2 (K = f+1 BFT-min)
		BFTStart:             0,
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		// Network is intentionally nil here. Validate() defaults it to
		// ConstantDelay{D: BTT}, and every sweep builder overrides
		// it to LogNormalDelay before running, so setting it here was
		// redundant — and risked implying the value was load-bearing.
		Host: HostAllValid{},
		Byz:  ByzPattern{Kind: ByzNone},
		Seed: 1,
		// Mesh: per-sim mesh tunables consumed when a scenario opts
		// into DeliveryMesh (Healthy and its instability-wrapped
		// variants do; every adversarial scenario stays direct).
		// HopDelay's Median = BTT/3 is the mesh-as-realism calibration
		// anchor — at n=4 with R=4 relays the typical hop count is ~2,
		// so total propagation ≈ 2·BTT/3 lands roughly where direct-
		// mode LogNormalDelay{Median: BTT/2} lands. Sigma is tighter
		// per-hop (0.3) because the cluster-wide heavy tail emerges
		// from the convolution rather than being baked into one draw.
		Mesh: MeshConfig{
			HopDelay: LogNormalDelay{Median: btt / 3, Sigma: 0.3},
		},
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
