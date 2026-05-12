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

// Protocol is the 2abOBFT adapter. Use as `twoab.Protocol{}` in tests.
type Protocol struct{}

func (Protocol) Name() string { return "2abOBFT" }

func (Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		// SimConfig.Validate covers schedule shape (BroadcastBudget,
		// FetchAt), T_commit positivity, and other operating-point-
		// derived constraints. Wrap as ErrConfigOutOfEnvelope so the
		// framework renders these cells as red 0% rather than n/a.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	// 2abOBFT splits Phase 2 into Phase 2a (verdict broadcast, Δ_2a ≥ 2 BTT
	// per spec §Setting) and Phase 2b (σ-or-NR propagation, Δ_2b ≥ 1 BTT;
	// recommended 2 BTT). We use spec-recommended sizings (Δ_2a = Δ_2b =
	// 2 BTT) and derive 2ab-shaped T_commit / BroadcastBudget / FetchAt
	// from cfg.RelayCutoff + cfg.BTT + cfg.K.
	//
	// The framework's cfg.Delta2 = 2 BTT (single-window OBFT shape) is
	// replaced here — 2ab's Phase 2 total budget is 4 BTT. T_commit lands
	// 2 BTT earlier than OBFT's at the same RelayCutoff (the spec's
	// "cost" for the validity-divergence safety story).
	delta2a := 2 * cfg.BTT
	delta2b := 2 * cfg.BTT
	delta2 := delta2a + delta2b
	tCommit := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom - cfg.Phase3JitterBuffer - cfg.Epsilon3 - delta2
	if tCommit <= 0 {
		// Operating-point-incompatible (BTT too large for the 2ab
		// Δ_2a + Δ_2b phase-2 tax to fit before RelayCutoff). Wrap as
		// ErrConfigOutOfEnvelope so the cell renders red 0% rather
		// than as an unexpected error.
		return ct.Outcome{}, fmt.Errorf("%w: twoab adapter: derived T_commit=%v is non-positive (RelayCutoff=%v BTT=%v)",
			ct.ErrConfigOutOfEnvelope, tCommit, cfg.RelayCutoff, cfg.BTT)
	}
	tVerdictStart := tCommit - delta2a

	// Derive 2ab-shaped BroadcastBudget + FetchAt. The framework's Validate
	// already populated cfg.BroadcastBudget / cfg.FetchAt under an OBFT-shape
	// T_commit; we replace them with 2ab-anchored equivalents.
	//
	// At extreme degraded operating points the helper returns a schedule
	// with shallow B_k values exceeding TVerdictStart; the per-layer
	// runtime `T_broadcast_max_k = max(BFT_start, TVerdictStart − B_k)`
	// clamps those layers' broadcast targets at BFT_start. Errors here
	// are only the K<1 / BTT≤0 programmer-error class.
	broadcastBudget, err := twoab.DefaultBroadcastBudget(cfg.K, cfg.BTT, tVerdictStart)
	if err != nil {
		return ct.Outcome{}, fmt.Errorf("twoab adapter: derive BroadcastBudget: %w", err)
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
		N:                     cfg.N,
		K:                     cfg.K,
		Operators:             cfg.Operators,
		TCommit:               tCommit,
		Delta2a:               delta2a,
		Delta2b:               delta2b,
		Epsilon3:              cfg.Epsilon3,
		BTT:                   cfg.BTT,
		FetchAt:               fetchAt,
		BroadcastBudget:       broadcastBudget,
		Network:               cfg.Network,
		Host:                  cfg.Host,
		Byz:                   internal,
		Seed:                  cfg.Seed,
		TraceEnabled:          cfg.TraceEnabled,
		BLSKeys:    cfg.BLSKeys,
		Aggregator: ct.NewOfflineAggregator(cfg.N),
		Bandwidth:  &bw,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Aggregator, desCfg.Bandwidth)
	out.CommitAttestation = computeAttestation(cfg, out)
	// Match the OBFT/QBFT adapters: late-recovered decisions past
	// RelayCutoff − HeaderSubmitHeadroom can't be submitted in production
	// either, so the comparison clips them to MISS.
	ct.ClipLateDecision(&out, cfg.RelayCutoff-cfg.HeaderSubmitHeadroom)
	return out, nil
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
}

// rawOutcome is the 2ab-internal outcome before translation to ct.Outcome.
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
