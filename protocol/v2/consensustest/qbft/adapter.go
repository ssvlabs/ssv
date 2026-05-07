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

// Protocol is the QBFT adapter. Use as `qbft.Protocol{}` in tests.
type Protocol struct {
	// MaxRounds caps round-change attempts before giving up. Default 4.
	MaxRounds int
}

func (Protocol) Name() string { return "QBFT" }

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		return ct.Outcome{}, err
	}

	internal, err := translateByz(cfg.Byz)
	if err != nil {
		return ct.Outcome{}, err
	}

	rt := cfg.QBFTRoundTimeout
	maxRounds := p.MaxRounds
	if maxRounds == 0 {
		maxRounds = 4
	}

	// BFT_start anchored so R2 success fits the relay deadline. R2 path is
	// RT (R1 timer) + 1 BTT (ROUND_CHANGE prop) + 3 BTT (PROPOSE/PREPARE/COMMIT)
	// + 1 BTT (post-consensus headroom) past BFT_start. At Config A this
	// resolves to 0.9s.
	bftStart := cfg.RelayCutoff - rt - 5*cfg.BTT - cfg.HeaderSubmitHeadroom
	if bftStart < 0 {
		bftStart = 0
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:            cfg.N,
		Operators:    cfg.Operators,
		BTT:          cfg.BTT,
		RT:           rt,
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
