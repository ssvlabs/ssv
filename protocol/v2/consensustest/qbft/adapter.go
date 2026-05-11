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
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Protocol is the QBFT adapter. Use as `qbft.Protocol{}` (defaults to
// "QBFT" + computed-RT variant) or with explicit field overrides for the
// "QBFT-SSV" variant.
type Protocol struct {
	// MaxRounds caps round-change attempts before giving up. Default 4.
	MaxRounds int

	// VariantName overrides the protocol name reported by Name(). When
	// empty, defaults to "QBFT".
	VariantName string

	// UseFixedRT picks the round-timeout source:
	//   false (default) — RT = 3 × cfg.PhaseBudget (= 6·BTT at PhaseBudget
	//     defaults). Matches the OBFT-family budget convention where each
	//     phase is 2·BTT. Use for the "QBFT" research variant.
	//   true — RT = cfg.QBFTRoundTimeout (= 2s default). Matches production
	//     SSV QBFT (QuickTimeout in roundtimer/timer.go). Use for the
	//     "QBFT-SSV" variant.
	UseFixedRT bool
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "QBFT"
}

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		return ct.Outcome{}, err
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	var rt time.Duration
	if p.UseFixedRT {
		rt = cfg.QBFTRoundTimeout
	} else {
		// Computed RT = 3 phases × PhaseBudget. Phases are PROPOSE,
		// PREPARE, COMMIT; ROUND_CHANGE is a separate post-timer phase
		// not counted in RT. At BTT=300 / PhaseBudget=2·BTT this is
		// 1800ms; at BTT=200 it's 1200ms.
		rt = 3 * cfg.PhaseBudget
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
		BTT:          cfg.BTT,
		RT:           rt,
		PhaseBudget:  cfg.PhaseBudget,
		MaxRounds:    maxRounds,
		BFTStart:     bftStart,
		Network:      cfg.Network,
		Host:         cfg.Host,
		Byz:          internal,
		Seed:         cfg.Seed,
		TraceEnabled: cfg.TraceEnabled,
		Bandwidth:    &bw,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Bandwidth)

	// The Instance runs round-changes regardless of wall time; the
	// application's relay cutoff is what makes long chains a slot miss in
	// practice. Clip post-deadline decisions to MISS so the outcome reflects
	// deployed-protocol behavior.
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	if out.Decided && out.DecisionTime > deadline {
		out.Decided = false
		out.DecidedRound = -1
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
	return out, nil
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
