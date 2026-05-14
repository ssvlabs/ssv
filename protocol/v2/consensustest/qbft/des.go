package qbft

import (
	"container/heap"
	"context"
	"fmt"
	mrand "math/rand"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	qbftcfg "github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftinstance "github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
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
	s.runLoop()
	return s.outcome(), nil
}

type sim struct {
	cfg                  desConfig
	rng                  *mrand.Rand
	now                  time.Duration
	queue                eventQueue
	seq                  int64
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
	trace                []ct.TraceEntry

	// Post-consensus partial-sig aggregation state. partials[receiver]
	// [valueKey] is the set of signers whose partial sig the receiver
	// has observed on that value. readyAt[receiver] is the earliest
	// time the receiver's signer-set on its decided value first
	// reached 2f+1 — "ready to submit" per the SSV proposer-duty
	// model. Outcome.DecisionTime = min(readyAt) across operators that
	// reached the quorum.
	partials map[spectypes.OperatorID]map[string]map[spectypes.OperatorID]bool
	readyAt  map[spectypes.OperatorID]time.Duration
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

	return &sim{
		cfg:        cfg,
		rng:        mrand.New(mrand.NewSource(cfg.Seed)),
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
		partials:             make(map[spectypes.OperatorID]map[string]map[spectypes.OperatorID]bool, cfg.N),
		readyAt:              make(map[spectypes.OperatorID]time.Duration, cfg.N),
	}, nil
}

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
	// Schedule each honest op to start at BFTStart. evtStartInstance triggers
	// Instance.Start, which arms the round-1 timer and (if op is round-1
	// proposer) emits the proposal.
	for _, op := range s.operators {
		if s.byz.IsByz(ct.OperatorID(op)) {
			continue
		}
		s.schedule(s.cfg.BFTStart, &evtStartInstance{op: op})
	}
	// Byz round-1 proposer is dispatched separately — the byz pattern's
	// ProposalPlanForRound returns the messages to fabricate. Goes through
	// scheduleByzProposal so the per-round dedup map records it.
	leader := proposerForRound(s.operators, specqbft.FirstRound)
	if s.byz.IsByz(ct.OperatorID(leader)) {
		s.scheduleByzProposal(s.cfg.BFTStart, leader, specqbft.FirstRound)
	}
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
	s.schedule(when, &evtByzProposal{leader: leader, round: round})
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

func (s *sim) runLoop() {
	// Cap virtual time at slot duration + headroom; events past that are dropped.
	maxTime := s.cfg.BFTStart + s.cfg.RT*time.Duration(s.cfg.MaxRounds+1)
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		if e.when > maxTime {
			break
		}
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{When: e.when, Event: e.ev.describe()})
		}
		newEvents := e.ev.handle(s)
		for _, ne := range newEvents {
			s.schedule(ne.when, ne.ev)
		}
	}
}

func (s *sim) schedule(when time.Duration, ev event) {
	s.seq++
	heap.Push(&s.queue, &queueItem{when: when, seq: s.seq, ev: ev})
}

func (s *sim) outcome() rawOutcome {
	out := rawOutcome{
		decided:      false,
		decidedRound: -1,
		perOp:        make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:        s.trace,
	}
	// "Ready to submit" semantic: an op is considered Decided only when
	// it has both (a) reached QBFT consensus locally AND (b) accumulated
	// 2f+1 partial sigs on the decided value. Outcome.DecisionTime is
	// the earliest "ready" time across the cluster, mirroring the
	// OBFT-family earliest-decider rule. An op that hit (a) but not (b)
	// is reported as not-decided with a "no postconsensus quorum"
	// diagnostic; classifyMiss surfaces this via shortenErr's
	// `no_postconsensus_quorum` label.
	earliestReady := time.Duration(-1)
	for _, op := range s.operators {
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
			oo.err = "no postconsensus quorum"
		} else if s.byz.IsByz(ct.OperatorID(op)) {
			// Byz operators don't run real Instances in this DES — the byz
			// pattern fabricates messages from them directly. They never
			// "decide", but it's not a failure either.
			oo.err = "byzantine — no instance"
		} else {
			oo.err = "did not decide before sim end"
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
// same receiver. Sets s.readyAt[receiver] = s.now the first time the
// distinct-signer count for `value` reaches quorum (2f+1).
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
	if len(sigs) >= s.quorum() {
		s.readyAt[receiver] = s.now
	}
}

// canonValueForRound returns a per-round canonical value. Different per round
// so a round-change with fresh-V proposes a different V (mirrors the prior
// behavioral adapter's semantics). Sized to a realistic Electra blinded
// block — see ct.RealisticBlindedBlockBytes. The leading byte encodes the
// round so distinct rounds produce distinct V's.
func (s *sim) canonValueForRound(round specqbft.Round) []byte {
	return ct.MakeRealisticBlindedBlockValue(byte(round))
}
