package obft

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
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
	operators   []obft.OperatorID
	cfgObft     *obft.Config
	pubShares   map[obft.OperatorID][]byte
	clusterPub  []byte
	instances   map[obft.OperatorID]*obft.Instance
	resolved    map[obft.OperatorID]*obft.Output
	resolvedAt  map[obft.OperatorID]time.Duration
	resolveErrs map[obft.OperatorID]error
	canonValues map[int]obft.Value
	trace       []ct.TraceEntry
}

func newSim(cfg desConfig) (*sim, error) {
	N := cfg.N
	operators := make([]obft.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = obft.OperatorID(cfg.Operators[i])
	}

	var pubShares map[obft.OperatorID][]byte
	var clusterPub []byte
	if cfg.BLSKeys != nil {
		pubShares = make(map[obft.OperatorID][]byte, N)
		for op, share := range cfg.BLSKeys.PubShares {
			pubShares[obft.OperatorID(op)] = share
		}
		clusterPub = cfg.BLSKeys.ClusterPubKey
	} else {
		pubShares = make(map[obft.OperatorID][]byte, N)
		for _, op := range operators {
			pubShares[op] = []byte{byte(op)}
		}
		clusterPub = []byte{0xCA, 0xFE}
	}

	canonValues := make(map[int]obft.Value, cfg.K)
	for k := 0; k < cfg.K; k++ {
		canonValues[k] = obft.Value(fmt.Sprintf("canon-V-layer-%d", k))
	}

	return &sim{
		cfg:         cfg,
		rng:         mrand.New(mrand.NewSource(cfg.Seed)),
		operators:   operators,
		pubShares:   pubShares,
		clusterPub:  clusterPub,
		canonValues: canonValues,
		instances:   make(map[obft.OperatorID]*obft.Instance, N),
		resolved:    make(map[obft.OperatorID]*obft.Output, N),
		resolvedAt:  make(map[obft.OperatorID]time.Duration, N),
		resolveErrs: make(map[obft.OperatorID]error, N),
	}, nil
}

func (s *sim) start() error {
	K := s.cfg.K
	layers := make([]obft.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obft.LayerSpec{
			Leader:          s.operators[k%s.cfg.N],
			FetchAt:         s.cfg.FetchAt[k],
			BroadcastBudget: s.cfg.BroadcastBudget[k],
		}
	}
	cfgObft := &obft.Config{
		Height:    1,
		ClusterID: [32]byte{0x01, 0x02, 0x03},
		Operators: s.operators,
		F:         (s.cfg.N - 1) / 3,
		Layers:    layers,
		TCommit:   s.cfg.TCommit,
		Delta2:    s.cfg.Delta2,
		Delta3:    s.cfg.Delta3,
		D:         s.cfg.D,
		Delta:     s.cfg.Delta,
	}
	if err := cfgObft.Validate(); err != nil {
		return fmt.Errorf("obft adapter: invalid OBFT config: %w", err)
	}
	s.cfgObft = cfgObft

	q := s.cfg.N - (s.cfg.N-1)/3
	useReal := s.cfg.BLSKeys != nil
	var ibe obft.ThresholdIBE
	if useReal {
		ibe = blsbackend.NewTLockIBE()
	} else {
		ibe = obft.NewStubIBE(q)
	}
	for _, op := range s.operators {
		var signer, tagSigner obft.Signer
		if useReal {
			share := s.cfg.BLSKeys.Shares[ct.OperatorID(op)]
			signer = blsbackend.New(share)
			tagSigner = blsbackend.NewKyberSigner(share)
		} else {
			stub := obft.NewStubSigner(q, []byte{byte(op)})
			signer = stub
			tagSigner = stub
		}
		inst, err := obft.NewInstance(cfgObft, op, signer, tagSigner, ibe, s.clusterPub, s.pubShares, nil)
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

func (s *sim) honestLeaderValue(layer int) obft.Value {
	return s.canonValues[layer]
}

// emitToAll schedules per-receiver arrival events for `from`'s message,
// honoring byz-pattern delivery / delay overrides. `bytes` is the wire size
// of the message (for bandwidth accounting; pass 0 to skip). `layer` is the
// OBFT layer for per-layer bandwidth accounting (use -1 for layer-agnostic).
func (s *sim) emitToAll(from obft.OperatorID, kind ct.MsgKind, layer int, bytes int64, build func(to obft.OperatorID) event) {
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
		s.schedule(s.now+delay, ev)
	}
}

func (s *sim) observedOffset() time.Duration { return s.now }
