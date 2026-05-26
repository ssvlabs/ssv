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

// defaultFixedRT is the QBFTSSV variant's flat round timeout — production
// SSV QBFT's QuickTimeout (roundtimer/timer.go).
const defaultFixedRT = 2 * time.Second

// defaultMaxRounds caps round-change attempts before the instance gives up.
// Set high enough that the slot deadline (runLoop's RelayCutoff-based maxTime
// + the adapter's clip), not the round count, is the binding limit across the
// sweep BTT range: the pristine per-round timers fit ~9-10 rounds within a 4s
// proposer slot at BTT=100, and we don't want to cap QBFT-0 below
// what it can actually fit. QBFT-SSV's wide 2s RT is deadline-bound at ~2-3
// rounds regardless, so the higher cap is a no-op for it.
const defaultMaxRounds = 12

// QBFT is the reflood-aware variant (Name "QBFT") — the realistic,
// production-shaped reference. Its per-round timer is the pristine
// structural minimum plus one full gossip-backstop recovery cushion
// (SafetyBuffer heartbeat + 1·BTT IWANT round-trip): round 1 =
// 3·BTT + (SafetyBuffer + 1·BTT) = 4·BTT + SafetyBuffer; rounds ≥ 2 add
// the 1·BTT ROUND_CHANGE hop = 5·BTT + SafetyBuffer. This matches OBFT's
// reflood budget (B_0 = 2·BTT + SafetyBuffer, whose 2·BTT base already
// carries the IWANT round-trip that QBFT's tight per-emission base lacks),
// so the two compare apples-to-apples on mesh-tail tolerance. Compare
// against bare OBFT (also reflood-aware).
type QBFT struct{}

func (QBFT) Name() string { return "QBFT" }

func (QBFT) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	// R1 = 3·BTT + (SafetyBuffer + 1·BTT cushion) = 4·BTT + SafetyBuffer;
	// rtRecoveryExtra = 1·BTT (the ROUND_CHANGE hop) → R≥2 = 5·BTT + SafetyBuffer.
	return run(cfg, 4*cfg.BTT+cfg.SafetyBuffer, cfg.BTT)
}

// QBFT0 is the pristine structural-floor variant, reported as
// "QBFT-0" (the 0-cushion rung of the QBFT ladder). Its round timer carries
// no mesh-tail cushion: round 1 = 3·BTT (the three consensus emissions
// PROPOSE + PREPARE + COMMIT at 1·BTT each — P99 propagation, matching
// OBFT's Δ_2 = 1·BTT); rounds ≥ 2 add 1·BTT for the ROUND_CHANGE hop to the
// new leader (the timer is armed at round entry, so that hop is inside the
// round's own window) = 4·BTT. Each round's timer equals that round's exact
// decision time, so a healthy/recovery decision coincides with the timer and
// eventQueue.Less (events.go) breaks the tie toward the decision — RT is
// never padded. Zero-cushion reference (compare against OBFT-0 / 2abOBFT-0).
// At BTT=100: R1=300ms, R≥2=400ms. NB this rung also drops the structural
// 1·BTT that the cushioned rungs carry, so the QBFT ladder is uneven:
// 3·BTT (QBFT-0) then 4·BTT+{300,500,700}.
type QBFT0 struct{}

func (QBFT0) Name() string { return "QBFT-0" }

func (QBFT0) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	return run(cfg, 3*cfg.BTT, cfg.BTT)
}

// QBFT300 is the 300ms-cushion rung, reported as "QBFT-300": the
// reflood-aware structure of bare QBFT with a fixed 300ms cushion.
// R1 = 4·BTT + 300ms, R≥2 = 5·BTT + 300ms. Analogue of OBFT-300 /
// 2abOBFT-300.
type QBFT300 struct{}

func (QBFT300) Name() string { return "QBFT-300" }

func (QBFT300) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	return run(cfg, 4*cfg.BTT+300*time.Millisecond, cfg.BTT)
}

// QBFT500 / QBFT700 are the 500ms / 700ms cushion rungs ("QBFT-500" /
// "QBFT-700"): same reflood-aware structure, fixed cushion, R1 = 4·BTT +
// {500,700}ms. QBFT-700 is the fixed-cushion analogue of the generic bare
// QBFT (which uses scenario cfg.SafetyBuffer = 700 on Healthy / 0 on
// adversarial); the stress matrix uses the explicit QBFT-700 rung so the
// cushion is named and scenario-independent.
type QBFT500 struct{}

func (QBFT500) Name() string { return "QBFT-500" }

func (QBFT500) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	return run(cfg, 4*cfg.BTT+500*time.Millisecond, cfg.BTT)
}

type QBFT700 struct{}

func (QBFT700) Name() string { return "QBFT-700" }

func (QBFT700) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	return run(cfg, 4*cfg.BTT+700*time.Millisecond, cfg.BTT)
}

// QBFTSSV is the production variant (Name "QBFT-SSV"): a flat 2s round
// timeout for every round, mirroring SSV's QuickTimeout (roundtimer/timer.go).
// 2s already dwarfs the per-round structural cost, so there is no per-round
// recovery extra. This is the deployed-timing reference.
type QBFTSSV struct{}

func (QBFTSSV) Name() string { return "QBFT-SSV" }

func (QBFTSSV) Run(cfg ct.SimConfig) (ct.Outcome, error) {
	return run(cfg, defaultFixedRT, 0)
}

// run is the shared QBFT DES driver. rt is the round-1 timeout (and, for the
// flat-RT variant, every round's timeout); rtRecoveryExtra is added by
// timer.go for rounds > FirstRound (the ROUND_CHANGE preamble) and is 0 for
// the flat-RT variant.
func run(cfg ct.SimConfig, rt, rtRecoveryExtra time.Duration) (ct.Outcome, error) {
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
	// Layer crash suppression on top of the translated byz pattern: crashed
	// operators emit nothing and receive nothing (overlay), are absent from
	// the mesh topology (MakeMeshTopology), and are reported offline by
	// sim.outcome(). Composes with any Kind; the sets are disjoint per Validate.
	if len(cfg.Byz.Crashed) > 0 {
		internal = crashOverlay{internalByz: internal, crashed: newByzSet(cfg.Byz.Crashed)}
	}

	// One consensus emission = 1·BTT (P99 propagation), mirroring OBFT's
	// tightened Δ_2 = 1·BTT so the protocols compare on equal per-emission
	// footing — forwarded as cfg.BTT to the DES below.

	// QBFT's PROPOSE-start anchor is slot start (the DES at qbft/des.go
	// schedules evtStartInstance at offset 0 for every honest op). The
	// whole QBFT pipeline shifts wholesale with BFT_start, so the sim
	// runs at BFT_start=0 and the report UI shifts decision times post-hoc
	// to model later BFT_start values.

	bw := ct.NewBandwidthReport()
	desCfg := desConfig{
		N:               cfg.N,
		Operators:       cfg.Operators,
		Crashed:         cfg.Byz.Crashed,
		BTT:             cfg.BTT,
		RT:              rt,
		RTRecoveryExtra: rtRecoveryExtra,
		MaxRounds:       defaultMaxRounds,
		Network:         cfg.Network,
		Host:            cfg.Host,
		Byz:             internal,
		Seed:            cfg.Seed,
		TraceEnabled:    cfg.TraceEnabled,
		Bandwidth:       &bw,
		Mesh:            cfg.MakeMeshTopology(),
		RelayCutoff:     cfg.RelayCutoff,
	}

	rawOut, err := runDES(desCfg)
	if err != nil {
		return ct.Outcome{}, err
	}
	out := rawOut.toCT(desCfg.Bandwidth)
	// Deep-copy via Clone — see OBFT base adapter for the rationale.
	out.Byz = cfg.Byz.Clone()
	out.CommitAttestation = computeAttestation(rawOut)

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

// computeAttestation populates Outcome.CommitAttestation from data the
// QBFT DES already exposes. Mirrors the OBFT / 2abOBFT adapters' pattern:
// each *Checked flag is set only when this adapter actually performs the
// corresponding cross-check; the framework treats unset flags as
// "uninstrumented, no violation reportable".
//
// Currently instrumented:
//   - Equivocation: counts distinct byz-leader PROPOSE values per round
//     (minus one per round) into EquivocationsObserved. The actual safety
//     check — that no honest op commits on an equivocated value without
//     also breaking agreement — is enforced upstream by the framework's
//     universal SingleV / HonestAgreement checks computed from PerOp. So
//     EquivocationsAccepted stays at 0 here; spec-QBFT's validation rules
//     reject equivocating proposals at the receiver, and any breach that
//     slipped through to a commit would manifest as a SingleV / HonestAgreement
//     violation upstream. EquivocationsObserved is diagnostic, distinguishing
//     vacuous-safe runs (==0) from tested-safe runs (>0). Mirrors the OBFT
//     adapter's diagnostic-only equivocation counter.
//
// Left uninstrumented (need spec-QBFT internals or extra plumbing — deferred):
//   - Quorum: would require plumbing the partial-signature count out of
//     the partials map (s.partials[receiver]); spec-qbft's Instance
//     internally enforces 2f+1 distinct partials before reporting decided.
//   - OBFTCommitKind / OBFTHostValidityRespect: OBFT-specific, not applicable
//     to QBFT (no σ-vs-NR cert dichotomy, no per-layer host re-validation).
func computeAttestation(raw rawOutcome) ct.CommitAttestation {
	return ct.CommitAttestation{
		EquivocationChecked:   true,
		EquivocationsObserved: raw.equivocationsObserved,
	}
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
		if oo.Err == DiagNoPostConsensusQuorum {
			return "Cluster agreed on a value, but never gathered enough post-consensus partial signatures"
		}
	}
	return "Cluster never reached consensus before slot end"
}

// desConfig is the QBFT-DES-internal configuration.
type desConfig struct {
	N               int
	Operators       []ct.OperatorID
	Crashed         []ct.OperatorID // completely-offline operators (subset of Operators)
	BTT             time.Duration
	RT              time.Duration // round-1 (base) round timeout
	RTRecoveryExtra time.Duration // added for rounds ≥ 2 (round-change hop); 0 for fixed-RT
	MaxRounds       int
	Network         ct.NetworkModel
	Host            ct.HostPattern
	Byz             internalByz
	Seed            int64
	TraceEnabled    bool
	Bandwidth       *ct.BandwidthReport
	Mesh            *ct.MeshTopology // nil when DeliveryDirect
	// RelayCutoff is the slot's hard submit deadline (carried over
	// from SimConfig). Used to bound the gossip-heartbeat sequence.
	RelayCutoff time.Duration
}

// rawOutcome is the QBFT-DES outcome before translation.
type rawOutcome struct {
	decided      bool
	decidedRound int
	decidedValue []byte
	decisionTime time.Duration
	perOp        map[ct.OperatorID]rawOpOutcome
	trace        []ct.TraceEntry
	// equivocationsObserved is the sim-side count of byz leader equivocation
	// events (distinct PROPOSE values emitted at the same round, minus one
	// per round). Set by sim.outcome() from s.equivocationsObserved. The
	// adapter exposes this via Outcome.CommitAttestation.EquivocationsObserved
	// so the framework can distinguish vacuous-pass from tested-pass for
	// equivocation-class scenarios.
	equivocationsObserved int
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
