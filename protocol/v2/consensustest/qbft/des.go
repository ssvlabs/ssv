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
		// startValue is what HONEST round-1 proposers propose. Distinct from
		// canonValueForRound(1) ("qbft-canon-V-round-1") so traces can tell
		// the two apart: byz round-1 leaders propose the per-round canon value
		// (handled by ProposalPlanForRound), making byz V's identifiable in
		// trace output. Both values are functionally equivalent (any V that
		// passes the value-checker), but their distinct strings make
		// "honest-leader vs byz-leader at round 1" debuggable from trace alone.
		startValue:           []byte("qbft-canon-V"),
		instances:            make(map[spectypes.OperatorID]*qbftinstance.Instance, cfg.N),
		timers:               make(map[spectypes.OperatorID]*virtualRoundTimer, cfg.N),
		decided:              make(map[spectypes.OperatorID]decidedRecord, cfg.N),
		inflightRound:        make(map[spectypes.OperatorID]specqbft.Round, cfg.N),
		byzProposalScheduled: make(map[specqbft.Round]bool),
		byz:                  cfg.Byz,
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
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		oo := rawOpOutcome{}
		if rec, ok := s.decided[op]; ok {
			oo.decided = true
			oo.value = append([]byte(nil), rec.value...)
			oo.round = int(rec.round)
			oo.time = rec.at
			if earliestT < 0 || rec.at < earliestT {
				earliestT = rec.at
				out.decided = true
				out.decidedValue = append([]byte(nil), rec.value...)
				out.decidedRound = int(rec.round)
				out.decisionTime = rec.at
			}
		}
		if !oo.decided {
			if s.byz.IsByz(ct.OperatorID(op)) {
				// Byz operators don't run real Instances in this DES — the byz
				// pattern fabricates messages from them directly. They never
				// "decide", but it's not a failure either.
				oo.err = "byzantine — no instance"
			} else {
				oo.err = "did not decide before sim end"
			}
		}
		out.perOp[ct.OperatorID(op)] = oo
	}
	return out
}

// canonValueForRound returns a per-round canonical value. Different per round
// so a round-change with fresh-V proposes a different V (mirrors the prior
// behavioral adapter's semantics).
func (s *sim) canonValueForRound(round specqbft.Round) []byte {
	return []byte(fmt.Sprintf("qbft-canon-V-round-%d", round))
}
