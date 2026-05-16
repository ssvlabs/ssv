// Package psigs is the PSigs (partial-sig only) adapter for consensustest.
// Models a single-round threshold partial-sig aggregation on a *pre-agreed*
// value V — no consensus on V, no rounds, no encrypted onion, no leader
// fall-through. Every honest operator signs V at slot start, broadcasts the
// partial sig to peers, and locally aggregates as partials arrive. An
// operator is "decided" the moment it holds qV = 2f+1 partials (including
// its own self-signature); the cluster's DecisionTime is the earliest such
// per-op time. The submission deadline is the same as the OBFT / QBFT
// adapters (RelayCutoff − HeaderSubmitHeadroom), so cells render on the
// same heatmap row for direct comparison.
//
// Use as the "baseline cost of partial-sig collection" curve against which
// OBFT / 2abOBFT / QBFT's full consensus overhead is measured. Most of the
// catalog's adversarial scenarios don't translate (no leader to silence /
// equivocate, no encrypted layer to fake-decrypt); see byz.go for the
// minimal set of byz patterns this adapter handles, and ErrNotApplicable
// for everything else.
//
// Safety invariants: trivially hold (only one V exists; threshold crypto's
// at-most-one-cluster-aggregate property is just qV-of-n unique partials),
// so the adapter does NOT populate Outcome.CommitAttestation. The
// framework's *Checked fields default to false and SafetyReport gracefully
// renders "n/a" for unchecked invariants.
package psigs

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Protocol is the PSigs adapter. Use as `psigs.Protocol{}`.
type Protocol struct {
	// VariantName overrides the protocol name reported by Name(). When
	// empty, defaults to "PSigs". Kept for parity with the OBFT / QBFT
	// adapters' variant convention, though PSigs has no protocol knobs
	// to vary (no BTTMultiplier, no fixed-RT vs computed-RT — the
	// protocol is a single broadcast + threshold-collect).
	VariantName string
}

func (p Protocol) Name() string {
	if p.VariantName != "" {
		return p.VariantName
	}
	return "PSigs"
}

func (p Protocol) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	if err := cfg.Validate(); err != nil {
		return ct.Outcome{}, fmt.Errorf("%w: %v", ct.ErrConfigOutOfEnvelope, err)
	}

	// PSigs doesn't model host application validity (V is pre-agreed —
	// there's nothing to validate). Any non-trivial Host pattern (operators
	// returning NV per phase / per layer) has no PSigs analog; surface
	// that explicitly so the catalog cell renders n/a rather than
	// misleadingly succeeding while the consensus protocols miss on the
	// same scenario.
	if _, allValid := cfg.Host.(ct.HostAllValid); !allValid && cfg.Host != nil {
		return ct.Outcome{}, fmt.Errorf("%w: PSigs has no host-validity model",
			ct.ErrNotApplicable)
	}

	internal, err := translateByz(cfg.Byz, cfg.BTT)
	if err != nil {
		return ct.Outcome{}, err
	}

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:            cfg.N,
		Operators:    cfg.Operators,
		BTT:          cfg.BTT,
		SlotStart:    cfg.SlotStart,
		Network:      cfg.Network,
		Byz:          internal,
		Seed:         cfg.Seed,
		TraceEnabled: cfg.TraceEnabled,
		Bandwidth:    &bw,
		Mesh:         cfg.MakeMeshTopology(),
	}

	rawOut := runDES(desCfg)
	out := rawOut.toCT(desCfg.Bandwidth)

	// Same submit-deadline semantic as OBFT / QBFT: a cluster that hasn't
	// aggregated qV partials by RelayCutoff − HeaderSubmitHeadroom can't
	// submit, regardless of how late the partials trickled in.
	preClipDecided := out.Decided
	preClipTime := out.DecisionTime
	deadline := cfg.RelayCutoff - cfg.HeaderSubmitHeadroom
	ct.ClipLateDecision(&out, deadline)
	if !out.Decided {
		out.MissReason = classifyPSigsMiss(preClipDecided, preClipTime, deadline)
	}
	return out, nil
}

// classifyPSigsMiss labels a non-decided PSigs outcome. PSigs has a
// single qV threshold and no rounds / layers, so the diagnostic space
// is just two regimes: partials reached qV but past the submit deadline
// (clipped), or qV was never reached within the slot (typically too
// many byz refusers + slow propagation).
func classifyPSigsMiss(preDecided bool, preTime, deadline time.Duration) string {
	if preDecided && preTime > deadline {
		// Single static label; the DecisionTime distribution carries
		// the per-iter overshoot. Avoids exploding the failure-
		// breakdown row count with per-ms granularity.
		return "Cluster ready to submit, past the submit deadline"
	}
	return "Cluster never gathered enough partial signatures"
}

// desConfig is the PSigs-DES-internal configuration.
type desConfig struct {
	N            int
	Operators    []ct.OperatorID
	BTT          time.Duration
	SlotStart    time.Duration // when each op signs + broadcasts; usually 0
	Network      ct.NetworkModel
	Byz          internalByz
	Seed         int64
	TraceEnabled bool
	Bandwidth    *ct.BandwidthReport
	Mesh         *ct.MeshTopology // nil when DeliveryDirect
}

// rawOutcome is the PSigs-DES outcome before translation to ct.Outcome.
type rawOutcome struct {
	decided      bool
	decidedValue []byte
	decisionTime time.Duration
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
}

type rawOpOutcome struct {
	decided bool
	value   []byte
	time    time.Duration
	err     string
}

func (r rawOutcome) toCT(bw *ct.BandwidthReport) ct.Outcome {
	// PSigs has no rounds / no layers — successful decision is always
	// "round 0" (the single partial-sig pass). Encoded as 0 on success,
	// -1 on miss, matching the framework convention used by OBFT/QBFT.
	out := ct.Outcome{
		Decided:      r.decided,
		DecisionTime: r.decisionTime,
		DecidedValue: append([]byte(nil), r.decidedValue...),
		DecidedRound: -1,
		PerOp:        make(map[ct.OperatorID]ct.OperatorOutcome, len(r.perOp)),
		Trace:        r.trace,
	}
	if r.decided {
		out.DecidedRound = 0
	}
	for op, oo := range r.perOp {
		bandwidthOut := int64(0)
		bandwidthIn := int64(0)
		if bw != nil {
			bandwidthOut = bw.PerOperatorOut[op]
			bandwidthIn = bw.PerOperatorIn[op]
		}
		round := -1
		if oo.decided {
			round = 0
		}
		out.PerOp[op] = ct.OperatorOutcome{
			Decided:      oo.decided,
			Value:        append([]byte(nil), oo.value...),
			Round:        round,
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
