// Package qbft is the QBFT adapter for consensustest. It wraps the real
// qbft.Instance under a virtual-time discrete-event simulator: each honest
// operator gets a real Instance driven through the framework's DES, with
// signing / network / timer interfaces substituted by virtual implementations.
//
// Byzantine operators do NOT instantiate a real Instance. Instead, the byz
// pattern fabricates SignedSSVMessage envelopes (using the byz's RSA key)
// and dispatches them via the simulator's network. This lets byz patterns
// emit messages that violate QBFT semantics (equivocation, late round
// changes, etc.) without forking the real qbft.Instance.
package qbft

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Default QBFT round timeout used by the UseFixedRT variant ("QBFT-SSV").
// Mirrors production SSV QBFT's QuickTimeout (roundtimer/timer.go).
const defaultFixedRT = 2 * time.Second

// Protocol is the QBFT adapter. Use as `qbft.Protocol{}` (defaults to
// "QBFT" + computed-RT variant) or with explicit field overrides for the
// "QBFT-SSV" variant.
type Protocol struct {
	// MaxRounds caps round-change attempts before giving up. Default 4.
	MaxRounds int

	// VariantName overrides the protocol name reported by Name(). When
	// empty, defaults to "QBFT".
	VariantName string

	// BTTMultiplier scales cfg.BTT internally before deriving PhaseBudget
	// (= 2·bttEff), and — when UseFixedRT=false — the round timeout
	// (RT = 3 × PhaseBudget = 6·bttEff). Zero → 1.0 (no scaling).
	// Matches the OBFT/2abOBFT variant convention so the whole protocol
	// family shares one "loose-vs-tight" knob.
	BTTMultiplier float64

	// UseFixedRT picks the round-timeout source:
	//   false (default) — RT = 3 × PhaseBudget (= 6·bttEff). Matches the
	//     OBFT-family budget convention where each phase is 2·BTT. Use
	//     for the "QBFT" research variant.
	//   true — RT = FixedRT (= 2s when zero). Matches production SSV
	//     QBFT (QuickTimeout in roundtimer/timer.go). Use for the
	//     "QBFT-SSV" variant. BTTMultiplier does NOT scale FixedRT —
	//     it's an absolute SSV-deployment constant, not a BTT-derived
	//     budget.
	UseFixedRT bool

	// FixedRT overrides the fixed-RT value when UseFixedRT=true. Zero
	// defaults to 2s. Has no effect when UseFixedRT=false.
	FixedRT time.Duration
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "QBFT"
}

// effectiveBTT applies the BTTMultiplier to cfg.BTT, clamped to ≥ 1ns.
// Zero multiplier → 1.0 (no scaling).
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
		// Mirror the OBFT / 2abOBFT adapters: SimConfig.Validate failures
		// at this point mean the operating point is incompatible with
		// QBFT. Wrap as ErrConfigOutOfEnvelope so cells render red 0%.
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	// PhaseBudget = 2·bttEff mirrors OBFT's Δ_2 convention so the two
	// protocols compare on equal-budget-per-phase footing under the
	// computed-RT variant. Used both for RT (computed) and for the
	// post-consensus margin in events.go.
	bttEff := p.effectiveBTT(cfg.BTT)
	phaseBudget := 2 * bttEff

	var rt time.Duration
	if p.UseFixedRT {
		rt = p.FixedRT
		if rt == 0 {
			rt = defaultFixedRT
		}
	} else {
		// Computed RT = 3 phases × PhaseBudget. Phases are PROPOSE,
		// PREPARE, COMMIT; ROUND_CHANGE is a separate post-timer phase
		// not counted in RT. At BTT=300 with multiplier=1 this is
		// 1800ms; at BTT=200 it's 1200ms.
		rt = 3 * phaseBudget
	}
	maxRounds := p.MaxRounds
	if maxRounds == 0 {
		maxRounds = 4
	}

	// BFT_start = 0 (slot start). Anchored to OBFT's earliest broadcast
	// time (FetchAt[K-1] ≈ 0 in the default schedule — the deepest /
	// lowest-MEV layer), which is the apples-to-apples reference point
	// for QBFT: both protocols start their "lowest-MEV" path at slot
	// start, then OBFT layer-walks toward L_0 while QBFT just runs R1.
	//
	// This also matches production SSV QBFT (proposer-role headStart=0
	// in roundtimer/timer.go). The R2 fallback still fits: R2 success
	// lands at RT + 4·BTT ≈ 3.2s at BTT=300ms, within the 3.9s
	// effective deadline.
	bftStart := time.Duration(0)

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:            cfg.N,
		Operators:    cfg.Operators,
		BTT:          bttEff,
		RT:           rt,
		PhaseBudget:  phaseBudget,
		MaxRounds:    maxRounds,
		BFTStart:     bftStart,
		Network:      cfg.Network,
		Host:         cfg.Host,
		Byz:          internal,
		Seed:         cfg.Seed,
		TraceEnabled: cfg.TraceEnabled,
		Bandwidth:    &bw,
		Mesh:         cfg.MakeMeshTopology(),
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Bandwidth)

	// Pre-clip snapshot: ClipLateDecision resets DecidedRound to -1, so
	// stash the round + time it actually reached for diagnostic labelling.
	preClipDecided := out.Decided
	preClipRound := out.DecidedRound
	preClipTime := out.DecisionTime
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	// The Instance runs round-changes regardless of wall time; the
	// application's relay cutoff is what makes long chains a slot miss in
	// practice. Clip post-deadline decisions to MISS so the outcome reflects
	// deployed-protocol behavior. Shared with the OBFT/2abOBFT adapters via
	// ct.ClipLateDecision so all three protocols honor the same submit
	// deadline (RelayCutoff − HeaderSubmitHeadroom).
	ct.ClipLateDecision(&out, deadline)
	if !out.Decided {
		out.MissReason = classifyQBFTMiss(out, preClipDecided, preClipRound, preClipTime, deadline)
	}
	return out, nil
}

// classifyQBFTMiss labels a non-decided QBFT outcome. Three regimes:
//
//   - Decided-but-clipped: at least one receiver reached the "ready to
//     submit" state (consensus + 2f+1 partials) but at preClipTime >
//     deadline. ClipLateDecision converted to MISS. Label:
//     "Cluster ready to submit at round <N>, past the submit deadline".
//   - Post-consensus quorum incomplete: some op(s) reached QBFT
//     consensus internally (their PerOp.Err is "no postconsensus
//     quorum" — set by the adapter's outcome() in des.go for the
//     "decided locally but no 2f+1 partial-sigs aggregated at any
//     receiver" case), but the cluster never hit "ready to submit".
//     Label: "Cluster agreed on a value, but never gathered enough
//     post-consensus partial signatures".
//   - Rounds exhausted: no op reached internal QBFT consensus at all
//     (all PerOp.Err are "did not decide before sim end" / byz). The
//     round-change chain ran through MaxRounds without convergence.
//     Label: "Cluster never reached consensus before slot end".
//
// QBFT rounds are 1-indexed in the spec, 0-indexed in the framework
// (DecidedRound = qbftRound - 1). The label adds 1 back so operators
// reading the report see the QBFT-spec round number.
func classifyQBFTMiss(out ct.Outcome, preDecided bool, preRound int, preTime, deadline time.Duration) string {
	if preDecided && preTime > deadline {
		return fmt.Sprintf("Cluster ready to submit at round %d, past the submit deadline", preRound+1)
	}
	// !preDecided. Distinguish by inspecting PerOp.Err patterns set by
	// the adapter's outcome() in des.go: "no postconsensus quorum" =>
	// at least one op reached internal consensus but the partial-sig
	// aggregation didn't quorum at any receiver. Anything else (the
	// "did not decide before sim end" or byz markers) means consensus
	// itself never reached.
	for _, oo := range out.PerOp {
		if oo.Err == "no postconsensus quorum" {
			return "Cluster agreed on a value, but never gathered enough post-consensus partial signatures"
		}
	}
	return "Cluster never reached consensus before slot end"
}

// desConfig is the QBFT-DES-internal configuration.
type desConfig struct {
	N            int
	Operators    []ct.OperatorID
	BTT          time.Duration
	RT           time.Duration
	PhaseBudget  time.Duration // per-phase budget (= 2·BTT default); used for post-cons margin
	MaxRounds    int
	BFTStart     time.Duration
	Network      ct.NetworkModel
	Host         ct.HostPattern
	Byz          internalByz
	Seed         int64
	TraceEnabled bool
	Bandwidth    *ct.BandwidthReport
	Mesh         *ct.MeshTopology // nil when DeliveryDirect
}

// rawOutcome is the QBFT-DES outcome before translation.
type rawOutcome struct {
	decided      bool
	decidedRound int
	decidedValue []byte
	decisionTime time.Duration
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
}

type rawOpOutcome struct {
	decided bool
	value   []byte
	round   int
	time    time.Duration
	err     string
}

func (r rawOutcome) toCT(bw *ct.BandwidthReport) ct.Outcome {
	// QBFT rounds are 1-indexed; framework's DecidedRound is 0-indexed
	// (matching OBFT's layer convention).
	normRound := func(qbftRound int) int {
		if qbftRound <= 0 {
			return -1
		}
		return qbftRound - 1
	}
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.decidedValue...),
		DecidedRound: normRound(r.decidedRound),
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	if !r.decided {
		out.DecidedRound = -1
	}
	for op, oo := range r.perOp {
		bandwidthOut := int64(0)
		bandwidthIn := int64(0)
		if bw != nil {
			bandwidthOut = bw.PerOperatorOut[op]
			bandwidthIn = bw.PerOperatorIn[op]
		}
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:      oo.decided,
			Value:        append([]byte(nil), oo.value...),
			Round:        normRound(oo.round),
			Time:         oo.time,
			Err:          oo.err,
			BandwidthOut: bandwidthOut,
			BandwidthIn:  bandwidthIn,
		}
	}
	if bw != nil {
		out.Bandwidth = *bw
	}
	return out
}
