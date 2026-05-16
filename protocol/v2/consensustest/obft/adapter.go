// Package obft is the OBFT adapter for consensustest. It wraps the real
// obft.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into OBFT-internal byz
// patterns.
package obft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Adapter-internal constants. NOT network-derived: ε_3 is the BLS-aggregation
// + IBE-decryption-walk CPU cost per fall-through layer (OBFT.md §Phase 3 /
// §Timing budget), and phase3JitterBuffer is the residual scheduling jitter
// between Phase 3 completion and cert/submit. Both are operator-side, so
// they don't scale with the BTT-multiplier (which models network slack only).
const (
	epsilon3           = 50 * time.Millisecond
	phase3JitterBuffer = 50 * time.Millisecond
)

// Protocol is the OBFT adapter. Use as `obft.Protocol{}` for the canonical
// variant, or with BTTMultiplier > 1 to model a "loose" deployment that
// over-budgets its internal timing assumptions relative to the network's
// actual BTT (see CAVEAT below).
type Protocol struct {
	// VariantName overrides the reported protocol name. Empty → "OBFT".
	// Used for registering Loose / MaxMEV-style variants alongside the
	// canonical adapter in the stress matrix without name collisions.
	VariantName string

	// BTTMultiplier scales cfg.BTT internally before deriving any timing
	// budget (Delta2, BroadcastBudget shallow layers, FetchAt fetch
	// buffer, and the BTT field forwarded to obftbase.Config). Zero is
	// treated as 1.0 (no scaling).
	//
	// CAVEAT — the multiplier affects the protocol's INTERNAL assumptions
	// only; the simulated network still propagates at cfg.BTT. Multiplier
	// > 1 ("loose") means the protocol budgets more slack per BTT-multiple
	// at the cost of an earlier T_commit (since Delta2 = 2·bttEff
	// consumes more of RelayCutoff); multiplier < 1 ("tight") is the
	// inverse trade. The CPU-side constants (epsilon3,
	// phase3JitterBuffer, cfg.HeaderSubmitHeadroom) do NOT scale —
	// they're operator-side reserves, not network propagation slack.
	BTTMultiplier float64

	// MaxMEVFetch removes the per-layer fetch buffer (the BTT/4 margin
	// between leader fetch and T_broadcast_max_k). When true, every leader
	// fetches and broadcasts exactly at its T_broadcast_max_k — the spec's
	// max-MEV operating point per OBFT.md §Timing budget. Mainly useful
	// for adapter-level unit tests exercising the spec's B_k decomposition
	// boundary; the stress driver doesn't register this variant.
	MaxMEVFetch bool
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "OBFT"
}

// effectiveBTT applies the BTTMultiplier to cfg.BTT, clamped to ≥ 1ns.
// Zero multiplier is treated as 1.0 (no scaling) so the zero-value
// Protocol{} behaves identically to the canonical OBFT.
func (p Protocol) effectiveBTT(btt time.Duration) time.Duration {
	mul := p.BTTMultiplier
	if mul <= 0 {
		mul = 1
	}
	out := time.Duration(float64(btt) * mul)
	if out < time.Nanosecond {
		out = time.Nanosecond
	}
	return out
}

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		// SimConfig.Validate covers cluster topology (N/K/Operators) and
		// the slot constants (BTT > 0, RelayCutoff > 0, RelayCutoff ≤
		// SlotDuration). Wrap as ErrConfigOutOfEnvelope so the framework
		// renders these cells as red 0% rather than n/a.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	// Derive every timing budget internally from bttEff = multiplier ·
	// cfg.BTT. The framework no longer carries Delta2 / BroadcastBudget /
	// FetchAt on SimConfig; OBFT family adapters own the spec's BTT-as-
	// unit conventions (Delta2 = 2·BTT recommended; B_k staggered as
	// 1·/1.5·/2.5·BTT; fetch buffer = BTT/4 default).
	bttEff := p.effectiveBTT(cfg.BTT)
	delta2 := 2 * bttEff
	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - phase3JitterBuffer - epsilon3 - delta2
	if tCommit <= 0 {
		return ct.Outcome{}, fmt.Errorf(
			"%w: obft adapter: derived T_commit=%v non-positive (RelayCutoff=%v HeaderSubmit=%v Phase3JitterBuffer=%v Epsilon3=%v Delta2=%v at BTTMultiplier=%v)",
			ct.ErrConfigOutOfEnvelope, tCommit, cfg.RelayCutoff, cfg.HeaderSubmitHeadroom,
			phase3JitterBuffer, epsilon3, delta2, p.BTTMultiplier)
	}

	broadcastBudget, err := obftbase.DefaultBroadcastBudget(cfg.K, bttEff, tCommit)
	if err != nil {
		return ct.Outcome{}, fmt.Errorf("%w: obft adapter: derive BroadcastBudget: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	// Cap shallow B_k at T_commit so the schedule stays non-decreasing
	// (the production helper doesn't clamp; the consensustest sim's DES
	// expects monotone fetch offsets).
	for k := range broadcastBudget {
		if broadcastBudget[k] > tCommit {
			broadcastBudget[k] = tCommit
		}
	}

	// Per-layer fetch buffer: bttEff/4 by default, 0 under MaxMEVFetch
	// (the spec's max-MEV-freshness boundary — leader fetches and
	// broadcasts at T_broadcast_max_k exactly).
	fetchBuffer := bttEff / 4
	if p.MaxMEVFetch {
		fetchBuffer = 0
	}
	fetchAt := make([]time.Duration, cfg.K)
	for k := 0; k < cfg.K; k++ {
		fa := tCommit - broadcastBudget[k] - fetchBuffer
		if fa < 0 {
			fa = 0
		}
		fetchAt[k] = fa
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:               cfg.N,
		K:               cfg.K,
		Operators:       cfg.Operators,
		TCommit:         tCommit,
		Delta2:          delta2,
		Epsilon3:        epsilon3,
		BTT:             bttEff,
		FetchAt:         fetchAt,
		BroadcastBudget: broadcastBudget,
		Network:         cfg.Network,
		Host:            cfg.Host,
		Byz:             internal,
		Seed:            cfg.Seed,
		TraceEnabled:    cfg.TraceEnabled,
		BLSKeys:         cfg.BLSKeys,
		Aggregator:      ct.NewOfflineAggregator(cfg.N),
		Bandwidth:       &bw,
		Mesh:            cfg.MakeMeshTopology(),
		RelayCutoff:     cfg.RelayCutoff,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Aggregator, desCfg.Bandwidth)
	out.CommitAttestation = computeAttestation(cfg, out)
	// Stamp the deciding-layer broadcast deadline so the reporting
	// layer's slot_start adjustment can model a late-joining operator
	// catching (or missing) the live broadcast. broadcastBudget[k] is
	// already clamped to ≤ tCommit upstream, so the subtraction never
	// goes negative.
	if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
		out.DecidingBroadcastTime = tCommit - broadcastBudget[out.DecidedRound]
	}
	// Capture pre-clip state so post-clip MissReason can reference the
	// layer the protocol DID internally reach (DecidedRound is reset to
	// -1 by ClipLateDecision; this snapshot preserves it for diagnostic).
	preClipDecided := out.Decided
	preClipRound := out.DecidedRound
	preClipTime := out.DecisionTime
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	// Clip late decisions to MISS so the outcome reflects deployed-protocol
	// behavior. Phase 3's evtResolveRerun can recover from a late-arriving
	// KindCommit past RoundEndOffset, but if the rebuilt decision lands past
	// RelayCutoff − HeaderSubmitHeadroom, production would miss the slot too
	// (no time left to broadcast the cert and submit). Mirrors the QBFT
	// adapter's deadline clip so the comparison is apples-to-apples.
	ct.ClipLateDecision(&out, deadline)
	if !out.Decided {
		out.MissReason = classifyOBFTMiss(preClipDecided, preClipRound, preClipTime, deadline)
	}
	return out, nil
}

// classifyOBFTMiss produces the friendly MissReason string for a non-
// decided Outcome. Two regimes:
//
//   - Decided-but-clipped: the protocol internally σ-resolved
//     (preClipDecided true) but at preClipTime > deadline. Label captures
//     the layer reached. Example: "Cluster ready to submit at layer 2,
//     past the submit deadline". The per-iter overshoot is preserved in
//     the DecisionTime distribution so the failure-breakdown row count
//     stays bounded (one row per layer instead of one per unique
//     millisecond of lateness).
//   - Never decided: the σ-pool never reached qV at any layer (NR-quorum
//     also short, or the walk exhausted K layers). Falls back to the
//     coarse "Cluster never assembled a threshold signature at any
//     layer" — the framework's per-op Err strings still carry
//     obft.Resolve()'s ErrNoQuorum for the diagnostic-curious.
func classifyOBFTMiss(preDecided bool, preRound int, preTime, deadline time.Duration) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at layer %d, past the submit deadline", preRound)
	}
	return "Cluster never assembled a threshold signature at any layer"
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Each *Checked flag is set only when this
// adapter actually performs the corresponding cross-check; the framework
// treats unset flags as "uninstrumented, no violation reportable".
//
// Currently instrumented:
//   - Equivocation: Rule2 (LeaderEquivocation) + Rule3 (CrossOnion /
//     CommitEquivocation) evidence fires are counted into
//     EquivocationsObserved. EquivocationsAccepted stays at 0 — OBFT's
//     internal Rule3 enforcement excludes equivocating partials from σ /
//     NR quorums by construction, so any actually-accepted equivocation
//     would already manifest as a NoOfflineDoubleV / SingleV violation
//     upstream. The framework therefore needs no additional gate here; the
//     EquivocationsObserved count is diagnostic, distinguishing
//     "vacuously safe" runs (==0) from "tested safe" runs (>0).
//
// Left uninstrumented (need deeper introspection than the adapter boundary
// currently exposes — deferred to a follow-up):
//   - Quorum: would require plumbing the partial-signature count out of
//     obft.Instance.BuildCertificate. obft.Instance internally enforces
//     ≥ 2f+1 distinct valid partials before emitting Output; that
//     invariant is correctness-of-protocol, not currently re-verified at
//     the framework level.
//   - OBFTCommitKind: distinguishing σ-quorum-commit from NR-quorum-commit
//     requires inspecting which path obft.Instance took to build the cert
//     (direct L_0 σ-quorum vs. NR-unlocked deeper σ-reconstruction). The
//     final cert is always a σ-signature on V regardless of path.
//   - OBFTHostValidityRespect: OBFT's validate-once-and-lock property means
//     a layer-naive comparison (decided_layer vs current host verdict)
//     over-reports — scenarios like HostFlipMidSlot have ops legitimately
//     decide at L_1 on a V they accepted at L_0 when the host's L_1
//     verdict is "invalid". A correct check requires plumbing each op's
//     recorded acceptance-layer through the DES boundary.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
	}

	for _, oo := range out.PerOp {
		for rule, n := range oo.EvidenceByRule {
			if rule == RuleLeaderEquivocation ||
				rule == RuleCrossOnionEquivocation ||
				rule == RuleCommitEquivocation {
				att.EquivocationsObserved += n
			}
		}
	}

	return att
}

// desConfig is the OBFT-DES-internal configuration, built by Run.
type desConfig struct {
	N               int
	K               int
	Operators       []ct.OperatorID
	TCommit         time.Duration
	Delta2          time.Duration
	Epsilon3        time.Duration // forwarded to obftbase.Config.Delta3 (= ε_3 per spec)
	BTT             time.Duration
	FetchAt         []time.Duration
	BroadcastBudget []time.Duration
	Network         ct.NetworkModel
	Host            ct.HostPattern
	Byz             internalByz
	Seed            int64
	TraceEnabled    bool
	BLSKeys         *ct.BLSKeys
	Aggregator      *ct.OfflineAggregator
	Bandwidth       *ct.BandwidthReport
	// Mesh is nil when SimConfig.Delivery == DeliveryDirect (the default);
	// non-nil when the scenario opted into mesh transport. The sim's
	// publish/forward paths branch on Mesh != nil.
	Mesh *ct.MeshTopology
	// RelayCutoff is the slot's hard submit deadline (carried over
	// from SimConfig). Used by scheduleInitialHeartbeats to bound the
	// gossip heartbeat sequence — no point firing heartbeats past the
	// moment the slot's decision is moot.
	RelayCutoff time.Duration
}

// rawOutcome is the OBFT-internal outcome before translation to ct.Outcome.
type rawOutcome struct {
	decided      bool
	decisionTime time.Duration
	layer        int
	value        []byte
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
}

type rawOpOutcome struct {
	decided        bool
	layer          int
	value          []byte
	time           time.Duration
	err            string
	evidenceByRule map[string]int
}

func (r rawOutcome) toCT(agg *ct.OfflineAggregator, bw *ct.BandwidthReport) ct.Outcome {
	// rawOutcome.layer is initialized to -1 in outcome() when no operator
	// decided, so DecidedRound is correct without a separate post-fixup.
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.value...),
		DecidedRound: r.layer,
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	for op, oo := range r.perOp {
		var evMap map[string]int
		if len(oo.evidenceByRule) > 0 {
			evMap = make(map[string]int, len(oo.evidenceByRule))
			for k, v := range oo.evidenceByRule {
				evMap[k] = v
			}
		}
		bandwidthOut := int64(0)
		bandwidthIn := int64(0)
		if bw != nil {
			bandwidthOut = bw.PerOperatorOut[op]
			bandwidthIn = bw.PerOperatorIn[op]
		}
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:        oo.decided,
			Value:          append([]byte(nil), oo.value...),
			Round:          oo.layer,
			Time:           oo.time,
			Err:            oo.err,
			EvidenceByRule: evMap,
			BandwidthOut:   bandwidthOut,
			BandwidthIn:    bandwidthIn,
		}
	}
	if agg != nil {
		out.OfflineAgg = agg.AttemptAll()
	}
	if bw != nil {
		out.Bandwidth = *bw
	}
	return out
}

// valuePrefix returns a hex-prefix of v for diagnostic dumps. Distinct from
// the consensustest package's hashValue (which returns [32]byte for use as
// map keys); these serve different purposes and shouldn't share a name.
func valuePrefix(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}

// evidenceByRule maps a slice of obft.Evidence to per-rule fire counts using
// the framework's standard "OBFT/RuleN/Description" key convention.
func evidenceByRule(evs []obftbase.Evidence) map[string]int {
	if len(evs) == 0 {
		return nil
	}
	m := make(map[string]int)
	for _, e := range evs {
		m[ruleKey(e)]++
	}
	return m
}

// Rule-name constants for OperatorOutcome.EvidenceByRule keys. Shared
// between the per-emission classifier (ruleKey) and the consumer
// instrumentation (computeAttestation) so a rename happens in one place.
const (
	RuleCrossSigning           = "OBFT/Rule1/CrossSigning"
	RuleLeaderEquivocation     = "OBFT/Rule2/LeaderEquivocation"
	RuleCommitEquivocation     = "OBFT/Rule3/CommitEquivocation"
	RuleCrossOnionEquivocation = "OBFT/Rule3/CrossOnionEquivocation"
	RuleFakeEncryptedPresence  = "OBFT/Rule4/FakeEncryptedPresence"
	RuleFakePlaintextSigma     = "OBFT/Rule5/FakePlaintextSigma"
	RuleUnknown                = "OBFT/Unknown"
)

func ruleKey(e obftbase.Evidence) string {
	switch e.Rule {
	case obftbase.EvidenceCrossSigning:
		return RuleCrossSigning
	case obftbase.EvidenceLeaderEquivocation:
		return RuleLeaderEquivocation
	case obftbase.EvidenceCrossOnionEquivocation:
		// Layer == -1 indicates the top-level CommitEquivocation variant
		// (full Commit bodies); per-layer Layer ≥ 0 indicates the per-V σ
		// variant. Slashing layer treats them as the same fault but per-rule
		// telemetry distinguishes them.
		if e.Layer < 0 {
			return RuleCommitEquivocation
		}
		return RuleCrossOnionEquivocation
	case obftbase.EvidenceFakeEncryptedPresence:
		return RuleFakeEncryptedPresence
	case obftbase.EvidenceFakePlaintextSigma:
		return RuleFakePlaintextSigma
	default:
		return RuleUnknown
	}
}
