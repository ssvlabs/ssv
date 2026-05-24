package qbft

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/desim"
	qbftcfg "github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftinstance "github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// Per-op rawOpOutcome.Err string constants. Named so the adapter's
// classifyQBFTMiss + the adapter tests reference one symbol rather
// than string-equating literals. A future refactor that changes the
// human-readable phrasing now touches one place.
const (
	DiagNoPostConsensusQuorum    = "no postconsensus quorum"
	DiagByzantineNoInstance      = "byzantine — no instance"
	DiagDidNotDecideBeforeSimEnd = "did not decide before sim end"
)

// runDES executes one QBFT simulation under virtual time. Single-goroutine
// event loop with priority queue ordered by (timestamp, sequence) for
// determinism.
func runDES(cfg desConfig) (rawOutcome, error) {
	s, err := newSim(cfg)
	if err != nil {
		return rawOutcome{}, err
	}
	if err := s.start(); err != nil {
		return rawOutcome{}, err
	}
	// Cap virtual time at the relay cutoff plus one recovery round (see
	// Engine.RunUntil) — far enough to capture every round that can decide
	// before the deadline (decisions past RelayCutoff − headroom are clipped
	// to MISS by the adapter) plus the first round that lands just after it
	// (for miss labelling). NOT a function of RT × MaxRounds: the pristine
	// per-round RT is tiny, so that product would cut the sim off long before
	// the deadline and starve QBFT-0 of the later rounds it can fit.
	s.RunUntil(s, s.cfg.RelayCutoff+s.cfg.RT+s.cfg.RTRecoveryExtra)
	return s.outcome(), nil
}

type sim struct {
	*desim.Engine
	cfg                  desConfig
	operators            []spectypes.OperatorID
	keys                 *spectestingutils.TestKeySet
	committee            *spectypes.CommitteeMember
	identifier           []byte
	startValue           []byte
	instances            map[spectypes.OperatorID]*qbftinstance.Instance
	timers               map[spectypes.OperatorID]*virtualRoundTimer
	decided              map[spectypes.OperatorID]decidedRecord
	inflightRound        map[spectypes.OperatorID]specqbft.Round
	byzProposalScheduled map[specqbft.Round]bool // dedup: one byz PROPOSE per round
	byz                  internalByz
	// crashed[op] = true for completely-offline operators. Wire behavior is
	// suppressed by crashOverlay + the emitMesh guard; this set lets outcome()
	// exclude them from the decided set and stamp Err="offline".
	crashed map[spectypes.OperatorID]bool

	// Post-consensus partial-sig aggregation state. partials[receiver]
	// [valueKey] is the set of signers whose partial sig the receiver
	// has observed on that value. readyAt[receiver] is the earliest
	// time the receiver's signer-set on its decided value first
	// reached 2f+1 — "ready to submit" per the SSV proposer-duty
	// model. Outcome.DecisionTime = min(readyAt) across operators that
	// reached the quorum.
	partials map[spectypes.OperatorID]map[string]map[spectypes.OperatorID]bool
	readyAt  map[spectypes.OperatorID]time.Duration

	// equivocationsObserved counts byz leader equivocation events the
	// sim has actually emitted: per byz round, the number of DISTINCT
	// PROPOSE values dispatched minus one (a single-V plan adds zero;
	// a 2-V split adds one; a 3-V 1-1-1 split adds two). Summed across
	// every byz round that fires. Exposed via Outcome.CommitAttestation
	// so the framework can distinguish "vacuous safe" runs (==0) from
	// "tested safe" runs (>0) for equivocation-class scenarios — same
	// pattern OBFT uses with its Rule 2 / Rule 3 evidence counts.
	equivocationsObserved int
}

type decidedRecord struct {
	value []byte
	round specqbft.Round
	at    time.Duration
}

func newSim(cfg desConfig) (*sim, error) {
	keys, err := keysetForN(cfg.N)
	if err != nil {
		return nil, err
	}
	operators := make([]spectypes.OperatorID, cfg.N)
	for i := 0; i < cfg.N; i++ {
		operators[i] = spectypes.OperatorID(cfg.Operators[i])
	}
	committee := spectestingutils.TestingCommitteeMember(keys)

	crashed := make(map[spectypes.OperatorID]bool, len(cfg.Crashed))
	for _, op := range cfg.Crashed {
		crashed[spectypes.OperatorID(op)] = true
	}

	s := &sim{
		Engine:     desim.NewEngine(cfg.Seed, cfg.TraceEnabled),
		cfg:        cfg,
		operators:  operators,
		keys:       keys,
		committee:  committee,
		identifier: stableIdentifier(),
		// startValue is what HONEST round-1 proposers propose. Sized to a
		// realistic Electra blinded block (see ct.RealisticBlindedBlockBytes —
		// ~5 KB SSZ for the SignedBlindedBeaconBlock that SSV consensus
		// actually carries; the prior 12-byte placeholder underweighted
		// PROPOSE FullData bandwidth by ~3 orders of magnitude). The leading
		// byte (0xFF) is distinct from canonValueForRound's leading byte so
		// traces can still tell "honest round-1 V" apart from "byz/canon V"
		// at the same round (byz round-1 leaders propose canonValueForRound;
		// both pass value-check; byte 0 distinguishes them in traces).
		startValue:           ct.MakeRealisticBlindedBlockValue(0xFF),
		instances:            make(map[spectypes.OperatorID]*qbftinstance.Instance, cfg.N),
		timers:               make(map[spectypes.OperatorID]*virtualRoundTimer, cfg.N),
		decided:              make(map[spectypes.OperatorID]decidedRecord, cfg.N),
		inflightRound:        make(map[spectypes.OperatorID]specqbft.Round, cfg.N),
		byzProposalScheduled: make(map[specqbft.Round]bool),
		byz:                  cfg.Byz,
		crashed:              crashed,
		partials:             make(map[spectypes.OperatorID]map[string]map[spectypes.OperatorID]bool, cfg.N),
		readyAt:              make(map[spectypes.OperatorID]time.Duration, cfg.N),
	}
	// QBFT deprioritizes the round-timeout at equal virtual time so a decision
	// reachable the instant the timer fires wins: the COMMIT arrival is
	// processed first, drives the instance to decided, and the timer then
	// no-ops (evtRoundTimeout.handle bails on IsDecided). This is what lets the
	// pristine RT = 3·BTT floor (adapter.go) hold under ConstantDelay, where the
	// healthy decision and the timer both land at exactly 3·BTT.
	s.SetEqualTimeLowPriority(func(ev desim.Event) bool {
		_, ok := ev.(*evtRoundTimeout)
		return ok
	})
	return s, nil
}

// Mesh / Network / Bandwidth satisfy desim.Host (Now/Rng/Schedule/Run/Trace
// come from the embedded *desim.Engine).
func (s *sim) Mesh() *ct.MeshTopology         { return s.cfg.Mesh }
func (s *sim) Network() ct.NetworkModel       { return s.cfg.Network }
func (s *sim) Bandwidth() *ct.BandwidthReport { return s.cfg.Bandwidth }

func (s *sim) start() error {
	// Construct an Instance for each honest operator. Byz operators get no
	// Instance — the byz pattern fabricates messages from them directly.
	for _, op := range s.operators {
		if s.byz.IsByz(ct.OperatorID(op)) {
			continue
		}
		inst, err := s.buildInstance(op)
		if err != nil {
			return fmt.Errorf("qbft adapter: build instance op %d: %w", op, err)
		}
		s.instances[op] = inst
	}
	// Schedule each honest op to start at slot start (offset 0).
	// evtStartInstance triggers Instance.Start, which arms the round-1
	// timer and (if op is round-1 proposer) emits the proposal. The QBFT
	// pipeline shifts wholesale with BFT_start, so the sim runs at
	// BFT_start=0 and the report UI shifts decision times post-hoc.
	for _, op := range s.operators {
		if s.byz.IsByz(ct.OperatorID(op)) {
			continue
		}
		s.Schedule(0, &evtStartInstance{op: op})
	}
	// Byz round-1 proposer is dispatched separately — the byz pattern's
	// ProposalPlanForRound returns the messages to fabricate. Goes through
	// scheduleByzProposal so the per-round dedup map records it.
	leader := proposerForRound(s.operators, specqbft.FirstRound)
	if s.byz.IsByz(ct.OperatorID(leader)) {
		s.scheduleByzProposal(0, leader, specqbft.FirstRound)
	}
	desim.ScheduleInitialHeartbeats(s, s.cfg.RelayCutoff)
	return nil
}

// scheduleByzProposal enqueues exactly one evtByzProposal per (round) — even
// when called from each honest op's evtRoundTimeout (N times for N honest ops
// timing out at the same simulated instant), only the first call schedules
// the dispatch. Without this gate the same byz PROPOSE would fan out N times
// per round-change, inflating bandwidth and trace noise (protocol behavior is
// unchanged because spec-level dedup absorbs the duplicates).
func (s *sim) scheduleByzProposal(when time.Duration, leader spectypes.OperatorID, round specqbft.Round) {
	if s.byzProposalScheduled[round] {
		return
	}
	s.byzProposalScheduled[round] = true
	s.Schedule(when, &evtByzProposal{leader: leader, round: round})
}

func (s *sim) buildInstance(op spectypes.OperatorID) (*qbftinstance.Instance, error) {
	signer := newVirtualOperatorSigner(op, s.keys.OperatorKeys[op])
	timer := newVirtualRoundTimer(s, op)
	s.timers[op] = timer

	committee := *s.committee // shallow copy so each op has its own OperatorID
	committee.OperatorID = op

	cfg := &qbftcfg.Config{
		BeaconSigner: noopBeaconSigner{},
		Domain:       spectestingutils.TestingSSVDomainType,
		ProposerF: func(_ *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			return proposerForRound(s.operators, round)
		},
		Network:     newVirtualNetwork(s, op),
		CutOffRound: specqbft.Round(s.cfg.MaxRounds + 1),
	}

	inst := qbftinstance.NewInstance(
		context.Background(),
		zap.NewNop(),
		cfg,
		&committee,
		s.identifier,
		specqbft.FirstHeight,
		signer,
		func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
			return timer
		},
	)
	return inst, nil
}

func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		decided:               false,
		decidedRound:          -1,
		perOp:                 make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:                 s.Trace(),
		equivocationsObserved: s.equivocationsObserved,
	}
	// "Ready to submit" semantic: an op is considered Decided only when
	// it has both (a) reached QBFT consensus locally AND (b) accumulated
	// 2f+1 partial sigs on the decided value. Outcome.DecisionTime is
	// the earliest "ready" time across the cluster, mirroring the
	// OBFT-family earliest-decider rule. An op that hit (a) but not (b)
	// is reported as not-decided with a "no postconsensus quorum"
	// per-op diagnostic; classifyQBFTMiss inspects that diagnostic to
	// produce the cluster-level "Cluster agreed on a value, but never
	// gathered enough post-consensus partial signatures" MissReason.
	earliestReady := time.Duration(-1)
	for _, op := range s.operators {
		if s.crashed[op] {
			// Completely offline: never a decider, reported with Err="offline"
			// so Terminated stays satisfied and the op is excluded from
			// SingleV / HonestAgreement (decided-only checks).
			out.perOp[ct.OperatorID(op)] = rawOpOutcome{err: "offline"}
			continue
		}
		oo := rawOpOutcome{}
		rec, decidedLocally := s.decided[op]
		ready, hasReady := s.readyAt[op]
		if decidedLocally && hasReady {
			oo.decided = true
			oo.value = append([]byte(nil), rec.value...)
			oo.round = int(rec.round)
			oo.time = ready
			if earliestReady < 0 || ready < earliestReady {
				earliestReady = ready
				out.decided = true
				out.decidedValue = append([]byte(nil), rec.value...)
				out.decidedRound = int(rec.round)
				out.decisionTime = ready
			}
		} else if decidedLocally {
			// Consensus decided locally but no 2f+1 partial sigs landed
			// in time. The value is preserved as diagnostic context
			// (matches OBFT's ClipLateDecision: keep the decision body
			// even when the cluster missed the submit window).
			oo.value = append([]byte(nil), rec.value...)
			oo.round = int(rec.round)
			oo.err = DiagNoPostConsensusQuorum
		} else if s.byz.IsByz(ct.OperatorID(op)) {
			// Byz operators don't run real Instances in this DES — the byz
			// pattern fabricates messages from them directly. They never
			// "decide", but it's not a failure either.
			oo.err = DiagByzantineNoInstance
		} else {
			oo.err = DiagDidNotDecideBeforeSimEnd
		}
		out.perOp[ct.OperatorID(op)] = oo
	}
	return out
}

// quorum returns 2f+1 for the cluster size N (with f = (N-1)/3). Used
// by the partial-sig aggregation path to detect "ready to submit".
func (s *sim) quorum() int {
	f := (s.cfg.N - 1) / 3
	return 2*f + 1
}

// recordPartialSig records a partial-sig observation at `receiver` from
// `signer` on `value`. Idempotent on duplicate (signer, value) at the
// same receiver. Sets s.readyAt[receiver] = s.Now() the first time the
// distinct-signer count for `value` reaches quorum (2f+1) AND `receiver`
// has locally decided that same `value`.
//
// The decided+value gate matters under two conditions the framework can
// produce: (a) partials may legitimately arrive before the receiver's
// own consensus decision (mesh/direct delivery is concurrent with
// recordDecided's dispatch), and (b) byz equivocation can flood
// partials on a value the receiver did not decide. Without the gate,
// readyAt would stamp the earlier-of-the-two times — under-counting
// when partials race ahead of local decide, and outright wrong-attributing
// when peer partials hit quorum on a value the receiver isn't going to
// adopt.
//
// recordDecided's self-record path re-runs this function with
// (receiver=op, signer=op, value=decidedValue) AFTER setting
// s.decided[op], so any partials already buffered for the decided
// value get re-evaluated and readyAt fires at that moment.
func (s *sim) recordPartialSig(receiver, signer spectypes.OperatorID, value []byte) {
	if _, ready := s.readyAt[receiver]; ready {
		return
	}
	key := string(value)
	bucket := s.partials[receiver]
	if bucket == nil {
		bucket = make(map[string]map[spectypes.OperatorID]bool)
		s.partials[receiver] = bucket
	}
	sigs := bucket[key]
	if sigs == nil {
		sigs = make(map[spectypes.OperatorID]bool)
		bucket[key] = sigs
	}
	sigs[signer] = true
	if len(sigs) < s.quorum() {
		return
	}
	rec, decidedLocally := s.decided[receiver]
	if !decidedLocally {
		return
	}
	if string(rec.value) != key {
		// Quorum on a value the receiver didn't decide (byz
		// equivocation pool, or a legitimate value they rejected at
		// PROPOSE/PREPARE). The receiver can't submit this cert.
		return
	}
	s.readyAt[receiver] = s.Now()
}

// canonValueForRound returns a per-round canonical value. Different per round
// so a round-change with fresh-V proposes a different V (mirrors the prior
// behavioral adapter's semantics). Sized to a realistic Electra blinded
// block — see ct.RealisticBlindedBlockBytes. The leading byte encodes the
// round so distinct rounds produce distinct V's.
func (s *sim) canonValueForRound(round specqbft.Round) []byte {
	return ct.MakeRealisticBlindedBlockValue(byte(round))
}
