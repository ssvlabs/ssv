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
// between Phase 3 completion and cert/submit. Both are operator-side
// reserves, independent of the network's BTT.
const (
	epsilon3           = 50 * time.Millisecond
	phase3JitterBuffer = 50 * time.Millisecond
)

// Protocol is the OBFT adapter. Use as `obft.Protocol{}` for the canonical
// variant, or set SafetyBufferOverride to register cushion variants
// alongside it in the stress matrix without name collisions.
type Protocol struct {
	// VariantName overrides the reported protocol name. Empty → "OBFT".
	// Used for registering cushion variants alongside the canonical adapter
	// in the stress matrix without name collisions.
	VariantName string

	// SafetyBufferOverride, when non-nil, sets the broadcast budget's
	// SafetyBuffer to an explicit value regardless of cfg.SafetyBuffer.
	// Registers the cushion-ladder variants: OBFT-0 (override=0), OBFT-300
	// (override=300ms), OBFT-500 (override=500ms), OBFT-700 (override=700ms,
	// matches bare OBFT on Healthy). Analogue of 2abOBFT's
	// SafetyBufferOverride and QBFT-300's fixed cushion. Unlike 2abOBFT-300
	// (which collapses onto 2abOBFT-0 at BTT≥300 via the max(SB,BTT)
	// crossover), OBFT's B_0 = 2·BTT + SafetyBuffer is linear in SafetyBuffer,
	// so OBFT-300 stays distinct from OBFT-0 at every BTT.
	SafetyBufferOverride *time.Duration

	// BaselineOnly marks a variant that only runs on Baseline-group
	// (Healthy) scenarios — RunBatch renders it n/a on adversarial
	// scenarios. Set on the cushion-sensitivity rungs (X-0 / X-300 / X-500),
	// which exist to compare mesh-tail tolerance on Healthy; on adversarial
	// scenarios only the canonical X-700 rung runs (the cushion barely
	// affects adversarial recovery, which is structural).
	BaselineOnly bool
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "OBFT"
}

// IsBaselineOnly reports whether this variant runs only on Baseline-group
// scenarios (see BaselineOnly). Consumed by RunBatch.
func (p Protocol) IsBaselineOnly() bool { return p.BaselineOnly }

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
	// Layer crash suppression on top of the translated byz pattern: crashed
	// operators emit nothing and receive nothing (the overlay), are absent
	// from the mesh topology (MakeMeshTopology), and are reported offline by
	// sim.outcome(). Composes with any Kind; the sets are disjoint per Validate.
	if len(cfg.Byz.Crashed) > 0 {
		internal = crashOverlay{internalByz: internal, crashed: newByzSet(cfg.Byz.Crashed)}
	}

	// Derive every timing budget internally from cfg.BTT. The framework no
	// longer carries Delta2 / BroadcastBudget / FetchAt on SimConfig; OBFT
	// family adapters own the spec's BTT-as-unit conventions. Δ_2 = 1·BTT
	// matches the spec recommendation — reflood lives in B_0 (per-scenario
	// cfg.SafetyBuffer folded into the primary-layer broadcast budget), not
	// in Δ_2; see OBFT.md §Timing budget. Production SSV adapter uses the
	// same sizing.
	delta2 := cfg.BTT
	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - phase3JitterBuffer - epsilon3 - delta2
	if tCommit <= 0 {
		return ct.Outcome{}, fmt.Errorf(
			"%w: obft adapter: derived T_commit=%v non-positive (RelayCutoff=%v HeaderSubmit=%v Phase3JitterBuffer=%v Epsilon3=%v Delta2=%v)",
			ct.ErrConfigOutOfEnvelope, tCommit, cfg.RelayCutoff, cfg.HeaderSubmitHeadroom,
			phase3JitterBuffer, epsilon3, delta2)
	}

	// cfg.SafetyBuffer is the per-scenario opt-in for the spec's reflood-
	// absorption budget (`B_0 = 2·BTT + SafetyBuffer`). Default 0:
	// adversarial scenarios and direct-delivery sims model idealized
	// eager-push, no schedule-level reflood absorption. Production-
	// realistic-mesh scenarios set cfg.SafetyBuffer >0 (typically 700ms
	// to match libp2p heartbeat), mirroring the production SSV adapter's
	// DefaultSafetyBuffer. SafetyBufferOverride pins an explicit value
	// for the cushion-ladder variants (OBFT-0 / OBFT-300 / OBFT-500 /
	// OBFT-700) regardless of cfg.SafetyBuffer.
	safetyBuffer := cfg.SafetyBuffer
	if p.SafetyBufferOverride != nil {
		safetyBuffer = *p.SafetyBufferOverride
	}
	broadcastBudget, err := obftbase.DefaultBroadcastBudget(cfg.K, cfg.BTT, safetyBuffer, tCommit)
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

	// Per-layer broadcast schedule = obftbase.BroadcastTargetOffset: the
	// recovery-floored target max(0, T_commit − max(B_k, 3·BTT)). The 3·BTT
	// floor (2·BTT peer-reflood-V cascade + 1·BTT margin) keeps the primary
	// L_0's h_V=1 σ-upgrade landing before T_commit even at SafetyBuffer=0,
	// mirroring 2abOBFT's max(SafetyBuffer, 1·BTT). Shared with the production
	// runner (runner/obft/runner.go) so sim and production broadcast at the
	// identical floored target; backups (B_k = T_commit) floor to BFT_start.
	// Replaces the former flat BTT/4 fetchBuffer.
	fetchAt := make([]time.Duration, cfg.K)
	for k := 0; k < cfg.K; k++ {
		fetchAt[k] = obftbase.BroadcastTargetOffset(tCommit, broadcastBudget[k], cfg.BTT)
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:               cfg.N,
		K:               cfg.K,
		Operators:       cfg.Operators,
		Crashed:         cfg.Byz.Crashed,
		TCommit:         tCommit,
		Delta2:          delta2,
		Epsilon3:        epsilon3,
		BTT:             cfg.BTT,
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
	out.Byz = cfg.Byz
	out.CommitAttestation = computeAttestation(cfg, out)
	// Stamp the L_0 BFT_start-independence threshold = fetchAt[0] (the
	// recovery-floored L_0 broadcast target, obftbase.BroadcastTargetOffset)
	// — the largest BFT_start for which L_0's schedule is identical to
	// BFT_start=0. The report UI reuses this (BFT_start=0) cell at or below
	// this value (see Outcome.BFTStartIndependenceThreshold).
	//
	// The value is a pure function of cfg (BFT_start is not a sim
	// parameter — the sim always runs at BFT_start=0), so it's stamped
	// unconditionally on the single cell, regardless of decided/miss.
	bftIndep := fetchAt[0]
	out.BFTStartIndependenceThreshold = &bftIndep
	// Stamp the deciding-layer broadcast fire time = fetchAt[DecidedRound]
	// (the recovery-floored BroadcastTargetOffset — the actual broadcast,
	// which the floor may pull earlier than the B_k-budget deadline
	// T_commit − B_k). The reporting layer surfaces this as
	// `DecidingBroadcastTime` for the UI to label per-cell timing; matches
	// 2abOBFT, which reports its floored t0Broadcast.
	if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(fetchAt) {
		out.DecidingBroadcastTime = fetchAt[out.DecidedRound]
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
		out.MissReason = classifyOBFTMiss(preClipDecided, preClipRound, preClipTime, deadline, rawOut.deadlockLayer)
	}
	return out, nil
}

// classifyOBFTMiss produces the friendly MissReason string for a non-
// decided Outcome. Three regimes:
//
//   - Decided-but-clipped: the protocol internally σ-resolved
//     (preClipDecided true) but at preClipTime > deadline. Label captures
//     the layer reached. Example: "Cluster ready to submit at layer 2,
//     past the submit deadline". The per-iter overshoot is preserved in
//     the DecisionTime distribution so the failure-breakdown row count
//     stays bounded (one row per layer instead of one per unique
//     millisecond of lateness).
//   - Deadlock at layer X: at least one honest op's Resolve hit the
//     σ-pool-short + NR-pool-short stuck pattern (i.e. the HV1-style
//     OBFT-specific pathology that 2abOBFT recovers from via Phase-2a
//     NR-quorum). `deadlockLayer` carries the deepest such layer seen
//     across ops; the label surfaces it so operators can distinguish
//     this from a fully-silent cluster (next regime).
//   - Never decided / exhausted: every Resolve walked all K layers
//     without σ-quorum and without hitting the mid-walk stuck pattern.
//     Falls back to the coarse "Cluster never assembled a threshold
//     signature at any layer".
func classifyOBFTMiss(preDecided bool, preRound int, preTime, deadline time.Duration, deadlockLayer int) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at layer %d, past the submit deadline", preRound)
	}
	if deadlockLayer >= 0 {
		return fmt.Sprintf("Cluster deadlocked at layer %d (σ-pool short, no NR-fallthrough)", deadlockLayer)
	}
	return "Cluster never assembled a threshold signature at any layer"
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Each *Checked flag is set only when this
// adapter actually performs the corresponding cross-check; the framework
// treats unset flags as "uninstrumented, no violation reportable".
//
// Instrumented:
//   - OBFTCommitKindValid (C2): descriptive tag — L_0 decisions ⇒ "sigma";
//     L_k>0 decisions ⇒ "nr". Technically imprecise (L_k>0 σ-quorum can
//     theoretically reach via witness sections alone without NR-fallthrough)
//     but the existing OBFTCommitKindValid check only validates kind ∈
//     {"sigma", "nr"} — the imprecision is benign. Path-precise
//     classification would need obft.Output to carry a DecisionKind tag
//     from Resolve; deferred.
//   - Equivocation: Rule 2 (LeaderEquivocation) + Rule 3 (CrossOnion /
//     CommitEquivocation) evidence fires are counted into
//     EquivocationsObserved. Diagnostic — distinguishes "vacuously safe"
//     (==0) from "tested safe" (>0). EquivocationsAccepted stays at 0
//     (see C4 deferral note below).
//
// Left uninstrumented (need protocol-side instrumentation deeper than the
// adapter boundary exposes — separate follow-ups):
//   - QuorumBackedDecision (C1): the aggregator's view of cluster-wide
//     σ-pool cardinality (via OfflineAggReport.SigmaCardinality) is an
//     UNDERAPPROXIMATION of the protocol's local σ-pool view in scenarios
//     where the protocol combines plaintext leader-σ_V (delivered via
//     bundle, not via aggregator-recorded onion path) with chain-decrypted
//     peer partials, or where partition occludes message visibility to
//     specific receivers. False-positives flag legitimate decisions. A
//     correct C1 check needs the protocol to emit a per-decision quorum
//     count from obft.Instance.Resolve; deferred. The data plumbing
//     (SigmaCardinality on OfflineAggReport) is added in this commit for
//     future use.
//   - NoEquivocationAccepted (C4): OfflineAggregator's SigmaPartials
//     records every wire-emitted partial (regardless of Instance-side
//     Rule 3 filtering), so counting "dual emissions in pools" would
//     falsely flag legitimate ByzCrossSigning / ByzCrossOnionEquivocation
//     scenarios. A real count needs per-emitter decision-pool visibility
//     — bucket 2's SigmaByEmitter map enables this; deferred to that
//     commit. Rule 3 binary failure is transitively caught by
//     NoOfflineDoubleV.
//   - OBFTHostValidityRespect (C3): validate-once-and-lock means a
//     layer-naive comparison over-reports. Requires plumbing each op's
//     acceptance-layer through the DES boundary. Separate follow-up.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
	}

	if out.Decided {
		att.OBFTCommitKindChecked = true
		// C2 naive classification — descriptively imprecise but safety.go's
		// check only validates kind ∈ {"sigma", "nr"}.
		if out.DecidedRound == 0 {
			att.OBFTCommitKind = "sigma"
		} else {
			att.OBFTCommitKind = "nr"
		}
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
	Crashed         []ct.OperatorID // completely-offline operators (subset of Operators)
	TCommit         time.Duration
	Delta2          time.Duration
	Epsilon3        time.Duration // forwarded to obftbase.Config.Eps3 (= ε_3 per spec)
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
	// from SimConfig). Used by desim.ScheduleInitialHeartbeats to bound the
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
	// deadlockLayer carries the deepest layer at which any non-decided
	// op's Resolve hit the σ-pool-short + NR-pool-short stuck pattern
	// (obftbase.ResolveFailureDeadlock). -1 when no op deadlocked
	// (then classifyOBFTMiss falls back to the exhaustion label). Set
	// in sim.outcome() by inspecting s.resolveErrs.
	deadlockLayer int
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
