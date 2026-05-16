// Package twoab is the 2abOBFT adapter for consensustest. It wraps the
// real twoab.Instance under a virtual-time discrete-event simulator and
// translates abstract consensustest scenarios into 2ab-internal byz
// patterns.
//
// Structurally mirrors protocol/v2/consensustest/obft/ (the bare OBFT
// adapter) — same DES shape, same per-recipient cloning discipline,
// same evidence-rule-name convention. The 2ab-specific delta vs base
// is the Phase-2a verdict broadcast window: evtVerdictBroadcastStart
// fires at T_verdict_max - ε_proc and produces evtVerdictArrival events
// (replacing nothing in base — Phase-2a doesn't exist in bare OBFT),
// followed by evtOnion2bArrival at T_commit + propagation (replacing
// base's evtCommitArrival).
package twoab

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Adapter-internal constants. Mirrors the OBFT adapter — these are
// operator-side reserves (BLS aggregation / IBE walk CPU cost, residual
// scheduling jitter), so they don't scale with BTTMultiplier.
const (
	epsilon3           = 50 * time.Millisecond
	phase3JitterBuffer = 50 * time.Millisecond
)

// Protocol is the 2abOBFT adapter. Use as `twoab.Protocol{}` for the
// canonical variant, or with BTTMultiplier > 1 to model a "loose"
// deployment that over-budgets its internal timing assumptions relative
// to the network's actual BTT (see CAVEAT below).
type Protocol struct {
	// VariantName overrides the reported protocol name. Empty → "2abOBFT".
	VariantName string

	// BTTMultiplier scales cfg.BTT internally before deriving every
	// timing budget (Delta2a, Delta2b, BroadcastBudget shallow layers,
	// and the BTT field forwarded to twoab.Config — which 2ab uses to
	// compute its TAcceptMax = TCommit − 1·BTT receiver horizon). Zero
	// is treated as 1.0.
	//
	// CAVEAT — the multiplier affects the protocol's INTERNAL
	// assumptions only; the simulated network still propagates at
	// cfg.BTT. Multiplier > 1 ("loose") widens all four Phase-2 windows
	// (Δ_2a + Δ_2b = 4·bttEff total, vs 4·BTT canonical) and pushes
	// T_commit earlier in the slot by 2·(bttEff − cfg.BTT). The CPU-
	// side reserves (epsilon3, phase3JitterBuffer,
	// cfg.HeaderSubmitHeadroom) do NOT scale.
	BTTMultiplier float64
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "2abOBFT"
}

// effectiveBTT applies the BTTMultiplier to cfg.BTT, clamped to ≥ 1ns.
// Zero multiplier is treated as 1.0 so zero-value Protocol{} behaves
// identically to the canonical 2abOBFT.
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
		// the slot constants. Wrap as ErrConfigOutOfEnvelope so the
		// framework renders these cells as red 0% rather than n/a.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	// 2abOBFT splits Phase 2 into Phase 2a (verdict broadcast, Δ_2a ≥ 2 BTT
	// per spec §Setting) and Phase 2b (σ-or-NR propagation, Δ_2b ≥ 1 BTT;
	// recommended 2 BTT). Spec-recommended sizings (Δ_2a = Δ_2b = 2·bttEff)
	// give the 2ab Phase-2 total = 4·bttEff. T_commit lands 2·bttEff
	// earlier than OBFT's at the same RelayCutoff — the spec's "cost" for
	// the validity-divergence safety story.
	bttEff := p.effectiveBTT(cfg.BTT)
	delta2a := 2 * bttEff
	delta2b := 2 * bttEff
	delta2 := delta2a + delta2b
	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - phase3JitterBuffer - epsilon3 - delta2
	if tCommit <= 0 {
		// Operating-point-incompatible (bttEff too large for the 2ab
		// 4·BTT Phase-2 tax to fit before RelayCutoff). Wrap as
		// ErrConfigOutOfEnvelope so the cell renders red 0% rather
		// than as an unexpected error.
		return ct.Outcome{}, fmt.Errorf(
			"%w: twoab adapter: derived T_commit=%v non-positive (RelayCutoff=%v bttEff=%v at BTTMultiplier=%v)",
			ct.ErrConfigOutOfEnvelope, tCommit, cfg.RelayCutoff, bttEff, p.BTTMultiplier)
	}
	tVerdictStart := tCommit - delta2a
	if tVerdictStart <= 0 {
		// Even with T_commit positive, T_verdict_start can land before
		// slot 0 when Δ_2a > T_commit — i.e. the Phase-2a verdict
		// window would need to start before the slot exists. The
		// Phase-1 broadcast schedule collapses to BFT_start (FetchAt
		// clamps `< 0` to 0) and no leader can meet its target; every
		// sim fails to decide. Flag this band as OOE so it renders
		// red `Protocol cannot operate at this configuration` rather
		// than a stealth 0% success rate — the protocol genuinely
		// can't operate at this (bttEff, RelayCutoff, K) point.
		//
		// Boundary: let R = RelayCutoff − HeaderSubmitHeadroom −
		// Phase3JitterBuffer − Epsilon3 (the "available phase-2
		// budget", everything before Δ_2 is subtracted). Then
		// T_commit = R − (Δ_2a + Δ_2b) > 0 iff Δ_2a + Δ_2b < R, and
		// T_verdict_start = R − (2·Δ_2a + Δ_2b) > 0 iff 2·Δ_2a + Δ_2b
		// < R. This guard catches the narrow band
		//
		//   Δ_2a + Δ_2b  <  R  ≤  2·Δ_2a + Δ_2b
		//
		// where T_commit slips through positive but T_verdict_start is
		// non-positive. Concrete examples at R = 4000−100−50−50 =
		// 3800ms: 2abOBFTx3 at BTT=300 has Δ_2a = Δ_2b = 1800ms
		// (T_commit=200, T_verdict_start=−1600); 2abOBFTx2 at BTT=400
		// has Δ_2a = Δ_2b = 1600ms (T_commit=600, T_verdict_start=
		// −1000).
		return ct.Outcome{}, fmt.Errorf(
			"%w: twoab adapter: derived T_verdict_start=%v non-positive (T_commit=%v Delta2a=%v bttEff=%v at BTTMultiplier=%v)",
			ct.ErrConfigOutOfEnvelope, tVerdictStart, tCommit, delta2a, bttEff, p.BTTMultiplier)
	}

	// 2ab anchors B_k at TVerdictStart (= TCommit − Δ_2a), not TCommit —
	// the Phase-1 broadcast must complete before the Phase-2a verdict
	// window starts, not before TCommit. The DefaultBroadcastBudget
	// helper from the production package gives the spec's staggered
	// schedule against that anchor.
	//
	// At extreme degraded operating points the helper returns a schedule
	// with shallow B_k values exceeding TVerdictStart; the per-layer
	// runtime `T_broadcast_max_k = max(BFT_start, TVerdictStart − B_k)`
	// clamps those layers' broadcast targets at BFT_start. Errors here
	// are only the K<1 / BTT≤0 programmer-error class.
	// RefloodDelay=0: consensustest simulates idealized eager-push delivery;
	// gossipsub-mesh reflood scenarios are exercised via DeliveryMesh +
	// scenario machinery, not via the schedule's static reflood-absorption
	// budget.
	broadcastBudget, err := twoab.DefaultBroadcastBudget(cfg.K, bttEff, 0, tVerdictStart)
	if err != nil {
		return ct.Outcome{}, fmt.Errorf("%w: twoab adapter: derive BroadcastBudget: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	fetchAt := make([]time.Duration, cfg.K)
	for k := 0; k < cfg.K; k++ {
		fetchAt[k] = tVerdictStart - broadcastBudget[k]
		if fetchAt[k] < 0 {
			fetchAt[k] = 0
		}
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:               cfg.N,
		K:               cfg.K,
		Operators:       cfg.Operators,
		TCommit:         tCommit,
		Delta2a:         delta2a,
		Delta2b:         delta2b,
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
	// Stamp the deciding-layer broadcast deadline (anchored at
	// tVerdictStart, not tCommit, because the Phase-1 broadcast must
	// complete before Phase-2a opens). Shallow layers whose B_k exceed
	// tVerdictStart clamp to BFT_start, matching the runtime rule
	// T_broadcast_max_k = max(BFT_start, TVerdictStart − B_k).
	if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
		bt := tVerdictStart - broadcastBudget[out.DecidedRound]
		if bt < 0 {
			bt = 0
		}
		out.DecidingBroadcastTime = bt
	}
	// Pre-clip snapshot so MissReason can reference the layer the protocol
	// internally reached. See obft adapter's classifyOBFTMiss for the
	// shared rationale.
	preClipDecided := out.Decided
	preClipRound := out.DecidedRound
	preClipTime := out.DecisionTime
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	// Match the OBFT/QBFT adapters: late-recovered decisions past
	// RelayCutoff − HeaderSubmitHeadroom can't be submitted in production
	// either, so the comparison clips them to MISS.
	ct.ClipLateDecision(&out, deadline)
	if !out.Decided {
		out.MissReason = classifyTwoabMiss(preClipDecided, preClipRound, preClipTime, deadline, rawOut.deadlockLayer)
	}
	return out, nil
}

// classifyTwoabMiss mirrors the OBFT classifier; structurally identical
// (decided-but-clipped vs deadlocked-mid-walk vs exhausted-K-layers),
// since the 2ab Phase-3 walk has the same two failure modes. See
// classifyOBFTMiss in the obft adapter for the rationale on each
// regime. 2abOBFT's Phase-2a verdict step makes the HV1-style
// L_0 deadlock that bites OBFT recover via NR-quorum here, so in
// practice ResolveFailureDeadlock should be rare for 2abOBFT — when
// it appears it implies a different pathology than the OBFT case
// (e.g. σ AND NR pools BOTH degraded at the same layer).
func classifyTwoabMiss(preDecided bool, preRound int, preTime, deadline time.Duration, deadlockLayer int) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at layer %d, past the submit deadline", preRound)
	}
	if deadlockLayer >= 0 {
		return fmt.Sprintf("Cluster deadlocked at layer %d (σ-pool short, no NR-fallthrough)", deadlockLayer)
	}
	return "Cluster never assembled a threshold signature at any layer"
}

// computeAttestation populates Outcome.CommitAttestation from data already
// visible at the adapter boundary. Mirrors base's instrumentation: equivocation
// is counted from Rule 2 (LeaderEquivocation) + Rule 3 (per-layer
// CrossOnionEquivocation + top-level OnionEquivocation) + Rule 6a
// (VerdictEquivocation, 2ab-specific) evidence fires.
//
// Rule 6b (VerdictAction) is NOT counted as equivocation: per Rule 6b's
// boundary-conditional nature (honest revision is permitted), a fire there
// doesn't imply byzantine equivocation. It is observable separately via
// EvidenceByRule for tests that want to assert on it directly.
//
// Left uninstrumented (same as base; would require deeper Instance
// introspection):
//   - Quorum: would require plumbing the partial-signature count out of
//     twoab.Instance.BuildCertificate. The Instance internally enforces
//     ≥ 2f+1 distinct valid partials before emitting Output.
//   - 2abOBFTCommitKind (σ-quorum at L_0 vs NR-unlocked deeper σ): the
//     final cert is always a σ-signature on V regardless of path.
//   - OBFTHostValidityRespect: 2ab's host re-validation at Phase-2a /
//     Phase-2b sign time means the layer-naive comparison over-reports.
//     Requires plumbing per-op acceptance-layer through the DES boundary.
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
	att := ct.CommitAttestation{
		EquivocationChecked: true,
	}

	for _, oo := range out.PerOp {
		for rule, n := range oo.EvidenceByRule {
			if rule == RuleLeaderEquivocation ||
				rule == RuleCrossOnionEquivocation ||
				rule == RuleOnionEquivocation ||
				rule == RuleVerdictEquivocation {
				att.EquivocationsObserved += n
			}
		}
	}

	return att
}

// desConfig is the 2ab-DES-internal configuration, built by Run. Carries
// the 2ab-spec-shaped Phase-2 split (Delta2a + Delta2b) separately from
// the framework's single-window cfg.Delta2.
type desConfig struct {
	N               int
	K               int
	Operators       []ct.OperatorID
	TCommit         time.Duration
	Delta2a         time.Duration
	Delta2b         time.Duration
	Epsilon3        time.Duration // forwarded to twoab.Config.Delta3 (= ε_3 per spec)
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
	Mesh            *ct.MeshTopology // nil when DeliveryDirect
	// RelayCutoff is the slot's hard submit deadline (carried over
	// from SimConfig). Used to bound the gossip-heartbeat sequence.
	RelayCutoff time.Duration
}

// rawOutcome is the 2ab-internal outcome before translation to ct.Outcome.
type rawOutcome struct {
	decided      bool
	decisionTime time.Duration
	layer        int
	value        []byte
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
	// deadlockLayer mirrors the OBFT adapter's: deepest layer at which
	// any non-decided op hit ResolveFailureDeadlock. -1 when none.
	// See protocol/v2/consensustest/obft/adapter.go for the rationale.
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

// valuePrefix returns a hex-prefix of v for diagnostic dumps.
func valuePrefix(v []byte) string {
	if len(v) == 0 {
		return "<empty>"
	}
	if len(v) > 6 {
		return fmt.Sprintf("%x", v[:6])
	}
	return fmt.Sprintf("%x", v)
}

// evidenceByRule maps a slice of twoab.Evidence to per-rule fire counts.
func evidenceByRule(evs []twoab.Evidence) map[string]int {
	if len(evs) == 0 {
		return nil
	}
	m := make(map[string]int)
	for _, e := range evs {
		m[ruleKey(e)]++
	}
	return m
}

// Rule-name constants for OperatorOutcome.EvidenceByRule keys. Follow the
// "<Protocol>/Rule<N>/<Description>" convention shared with the OBFT
// adapter, distinguished by the "2abOBFT/" prefix.
const (
	RuleCrossSigning           = "2abOBFT/Rule1/CrossSigning"
	RuleLeaderEquivocation     = "2abOBFT/Rule2/LeaderEquivocation"
	RuleCrossOnionEquivocation = "2abOBFT/Rule3/CrossOnionEquivocation"
	RuleOnionEquivocation      = "2abOBFT/Rule3/OnionEquivocation" // top-level (Layer == -1)
	RuleFakeEncryptedPresence  = "2abOBFT/Rule4/FakeEncryptedPresence"
	RuleFakePlaintextSigma     = "2abOBFT/Rule5/FakePlaintextSigma"
	RuleVerdictEquivocation    = "2abOBFT/Rule6a/VerdictEquivocation"
	RuleVerdictAction          = "2abOBFT/Rule6b/VerdictAction"
	RuleUnknown                = "2abOBFT/Unknown"
)

func ruleKey(e twoab.Evidence) string {
	switch e.Rule {
	case twoab.EvidenceCrossSigning:
		return RuleCrossSigning
	case twoab.EvidenceLeaderEquivocation:
		return RuleLeaderEquivocation
	case twoab.EvidenceCrossOnionEquivocation:
		// Layer == -1 indicates the top-level OnionEquivocation variant
		// (full Onion2b bodies); per-layer Layer ≥ 0 indicates the per-V σ
		// variant. Slashing layer treats them as the same fault but per-rule
		// telemetry distinguishes them.
		if e.Layer < 0 {
			return RuleOnionEquivocation
		}
		return RuleCrossOnionEquivocation
	case twoab.EvidenceFakeEncryptedPresence:
		return RuleFakeEncryptedPresence
	case twoab.EvidenceFakePlaintextSigma:
		return RuleFakePlaintextSigma
	case twoab.EvidenceVerdictEquivocation:
		return RuleVerdictEquivocation
	case twoab.EvidenceVerdictAction:
		return RuleVerdictAction
	default:
		return RuleUnknown
	}
}
