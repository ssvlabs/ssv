package obft

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftbase "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
)

// runDES is the core DES loop for OBFT. Built around obft.Instance under
// virtual time — no time.Now / time.Sleep. Single-goroutine event loop
// with a priority queue ordered by (timestamp, sequence) for tie-break
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
	cfg         desConfig
	rng         *mrand.Rand
	now         time.Duration
	queue       eventQueue
	seq         int64
	operators   []obftbase.OperatorID
	cfgObft     *obftbase.Config
	pubShares   map[obftbase.OperatorID][]byte
	clusterPub  []byte
	instances   map[obftbase.OperatorID]*obftbase.Instance
	resolved    map[obftbase.OperatorID]*obftbase.Output
	resolvedAt  map[obftbase.OperatorID]time.Duration
	resolveErrs map[obftbase.OperatorID]error
	canonValues map[int]obftbase.Value
	trace       []ct.TraceEntry
}

func newSim(cfg desConfig) (*sim, error) {
	N := cfg.N
	operators := make([]obftbase.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = obftbase.OperatorID(cfg.Operators[i])
	}

	var pubShares map[obftbase.OperatorID][]byte
	var clusterPub []byte
	if cfg.BLSKeys != nil {
		pubShares = make(map[obftbase.OperatorID][]byte, N)
		for op, share := range cfg.BLSKeys.PubShares {
			pubShares[obftbase.OperatorID(op)] = share
		}
		clusterPub = cfg.BLSKeys.ClusterPubKey
	} else {
		pubShares = make(map[obftbase.OperatorID][]byte, N)
		for _, op := range operators {
			pubShares[op] = []byte{byte(op)}
		}
		clusterPub = []byte{0xCA, 0xFE}
	}

	// Per-layer candidate values sized to a realistic Electra blinded block.
	// Each layer's leader (in production) fetches an independent block from
	// their relay/beacon, so V_0..V_{K-1} are distinct candidates of similar
	// size. See ct.RealisticBlindedBlockBytes for the size derivation
	// (~5 KB SSZ-encoded SignedBlindedBeaconBlock at 8-att Electra max).
	canonValues := make(map[int]obftbase.Value, cfg.K)
	for k := 0; k < cfg.K; k++ {
		canonValues[k] = obftbase.Value(ct.MakeRealisticBlindedBlockValue(byte(k + 1)))
	}

	return &sim{
		cfg:         cfg,
		rng:         mrand.New(mrand.NewSource(cfg.Seed)),
		operators:   operators,
		pubShares:   pubShares,
		clusterPub:  clusterPub,
		canonValues: canonValues,
		instances:   make(map[obftbase.OperatorID]*obftbase.Instance, N),
		resolved:    make(map[obftbase.OperatorID]*obftbase.Output, N),
		resolvedAt:  make(map[obftbase.OperatorID]time.Duration, N),
		resolveErrs: make(map[obftbase.OperatorID]error, N),
	}, nil
}

func (s *sim) start() error {
	K := s.cfg.K
	layers := make([]obftbase.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obftbase.LayerSpec{
			Leader:          s.operators[k%s.cfg.N],
			FetchAt:         s.cfg.FetchAt[k],
			BroadcastBudget: s.cfg.BroadcastBudget[k],
		}
	}
	cfgObft := &obftbase.Config{
		Height:    1,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: s.operators,
		F:         (s.cfg.N - 1) / 3,
		Layers:    layers,
		TCommit:   s.cfg.TCommit,
		Delta2:    s.cfg.Delta2,
		Delta3:    s.cfg.Epsilon3, // production obftbase.Config.Delta3 is the pure ε_3 budget
		BTT:       s.cfg.BTT,
	}
	if err := cfgObft.Validate(); err != nil {
		// Validate failures at this point mean the SimConfig translates
		// to an operating-point-incompatible obft.Config (e.g. TCommit
		// too small for the broadcast deadline at degraded BTT, deepest
		// B_{K-1} below BFT-min). Wrap with ErrConfigOutOfEnvelope so
		// the framework renders the cell as red 0% rather than logging
		// an unexpected-error warning.
		return fmt.Errorf("%w: obft adapter: invalid OBFT config: %v",
			ct.ErrConfigOutOfEnvelope, err)
	}
	s.cfgObft = cfgObft

	q := s.cfg.N - (s.cfg.N-1)/3
	useReal := s.cfg.BLSKeys != nil
	var ibe obftbase.ThresholdIBE
	if useReal {
		ibe = blsbackend.NewTLockIBE()
	} else {
		ibe = obftbase.NewStubIBE(q)
	}
	for _, op := range s.operators {
		var signer, tagSigner obftbase.Signer
		if useReal {
			share := s.cfg.BLSKeys.Shares[ct.OperatorID(op)]
			signer = blsbackend.New(share)
			tagSigner = blsbackend.NewKyberSigner(share)
		} else {
			stub := obftbase.NewStubSigner(q, []byte{byte(op)})
			signer = stub
			tagSigner = stub
		}
		inst, err := obftbase.NewInstance(cfgObft, op, signer, tagSigner, ibe, s.clusterPub, s.pubShares, nil, nil)
		if err != nil {
			return fmt.Errorf("obft adapter: new instance op %d: %w", op, err)
		}
		s.instances[op] = inst
	}

	for k := 0; k < K; k++ {
		s.schedule(s.cfg.FetchAt[k], &evtLeaderFetch{layer: k})
	}
	s.schedule(cfgObft.TCommit, &evtPhaseTwoStart{})
	s.schedule(cfgObft.RoundEndOffset(), &evtResolve{})
	return nil
}

func (s *sim) runLoop() {
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, ct.TraceEntry{
				When:  e.when,
				Event: e.ev.describe(),
			})
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
		decided: false,
		layer:   -1,
		perOp:   make(map[ct.OperatorID]rawOpOutcome, len(s.operators)),
		trace:   s.trace,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		o := rawOpOutcome{decided: false, layer: -1}
		if res := s.resolved[op]; res != nil {
			o.decided = true
			o.layer = res.Layer
			o.value = append([]byte{}, res.Value...)
			o.time = s.resolvedAt[op]
			if earliestT < 0 || o.time < earliestT {
				earliestT = o.time
				out.decided = true
				out.layer = res.Layer
				out.value = append([]byte(nil), res.Value...)
				out.decisionTime = o.time
			}
		}
		if err, ok := s.resolveErrs[op]; ok {
			o.err = err.Error()
		}
		o.evidenceByRule = evidenceByRule(s.instances[op].Evidence())
		out.perOp[ct.OperatorID(op)] = o
	}
	return out
}

func (s *sim) honestLeaderValue(layer int) obftbase.Value {
	return s.canonValues[layer]
}

// emitToAll schedules per-receiver arrival events for `from`'s message,
// honoring byz-pattern delivery / delay overrides. `bytes` is the wire size
// of the message (for bandwidth accounting; pass 0 to skip). `layer` is the
// OBFT layer for per-layer bandwidth accounting (use -1 for layer-agnostic).
// `extraDelay` is added on top of the per-pair network delay — used by byz
// patterns to push a specific operator's own-emission past a protocol
// deadline (e.g. OverrideOwnCommitDispatchDelay for late KindCommit
// scenarios).
func (s *sim) emitToAll(from obftbase.OperatorID, kind ct.MsgKind, layer int, bytes int64, extraDelay time.Duration, build func(to obftbase.OperatorID) event) {
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, ct.OperatorID(from), ct.OperatorID(to), kind)
		}
		if s.cfg.Bandwidth != nil && bytes > 0 {
			s.cfg.Bandwidth.Emission(ct.OperatorID(from), ct.OperatorID(to), kind, layer, bytes)
		}
		ev := build(to)
		s.schedule(s.now+delay+extraDelay, ev)
	}
}

func (s *sim) observedOffset() time.Duration { return s.now }
