// Package stresstest is a virtual-time discrete-event simulator for OBFT.
//
// Goal: drive `obft.Instance`s through a single slot at scale, with no
// wall-clock dependencies, so we can sweep (BFT_start, D) × byz-pattern ×
// host-pattern matrices and classify outcomes against the predictions in
// docs/BFT-comparison.md. Per-simulation determinism comes from (a) virtual
// time (no time.Sleep / time.Now), (b) a single-goroutine event loop with
// a priority queue ordered on (timestamp, sequence-number), (c) seeded
// math/rand for any randomness in the network/byz patterns. Sims are
// independent and can be run in parallel as goroutines without interfering
// with each other's virtual clocks.
//
// Use:
//
//	out := Run(SimConfig{...})
//	out.Decided / out.Layer / out.Value / out.Evidence ...
//
// The `obft.Instance` API is exercised directly; this harness doesn't go
// through the SSV adapter (`protocol/v2/ssv/runner/obft`) — the adapter's
// real-clock orchestration is tested separately.
package stresstest

import (
	"container/heap"
	"crypto/sha256"
	"fmt"
	mrand "math/rand"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"

	obft "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// SimConfig parameterizes one simulation.
type SimConfig struct {
	// N is the cluster size; F = (N-1)/3 is implied. Cluster operator
	// IDs are 1..N.
	N int
	// K is the layer count. Must be >= 3 (OBFT minimum).
	K int

	// TCommit / Delta2 / Delta3 / D / Delta are the OBFT timing
	// parameters. All offsets are relative to BFT_start (= virtual time 0
	// inside the sim).
	TCommit time.Duration
	Delta2  time.Duration
	Delta3  time.Duration
	D       time.Duration
	Delta   time.Duration

	// FetchAt provides the per-layer leader fetch offsets. Length K;
	// must be non-increasing (T_{K-1} <= ... <= T_0) and each <=
	// TCommit - 2*(D+Delta). When nil, a default schedule is used.
	FetchAt []time.Duration

	// Network is the propagation model; defaults to ConstantDelay{D}.
	Network NetworkModel

	// Byz is the byzantine pattern (one or zero byz operators). Defaults
	// to ByzNone.
	Byz ByzPattern

	// Host is the host application validity-verdict model.
	// Defaults to HostAllValid.
	Host HostPattern

	// Seed is the deterministic RNG seed. Same seed + same config →
	// byte-identical event trace.
	Seed int64

	// TraceEnabled records every dispatched event into Outcome.Trace.
	// Default off (cheap for production tests; turn on when an
	// assertion fails to replay).
	TraceEnabled bool

	// BLSKeys, when non-nil, switches the sim to real BLS + TLockIBE
	// crypto. Used to verify cryptographic invariants (Rule 4 detection
	// via real chain-decrypt failure, real partial-sig verification, etc.)
	// that the stub doesn't catch with full fidelity. Generate with
	// GenerateBLSKeys; reuse across many sims of the same cluster.
	BLSKeys *BLSKeys
}

// BLSKeys holds a generated threshold-shared BLS keypair for the cluster:
// the master pubkey, and the per-operator (secret-share, public-share)
// pair. Used by SimConfig.BLSKeys to opt into real BLS / TLockIBE.
type BLSKeys struct {
	ClusterPubKey []byte                       // herumi-format master pubkey
	Shares        map[obft.OperatorID][]byte   // herumi-format secret shares (per operator)
	PubShares     map[obft.OperatorID][]byte   // herumi-format public shares (per operator)
}

// GenerateBLSKeys produces a fresh threshold-shared BLS keypair for a
// cluster of `operators` at threshold q = 2f + 1 = 2*(N-1)/3 + 1.
//
// Generation is process-global (initializes herumi/bls once via
// threshold.Init). Generated keys can be reused across many sims of the
// same cluster — in fact reuse is recommended since key generation is
// the slowest operation in this package.
func GenerateBLSKeys(operators []obft.OperatorID) (*BLSKeys, error) {
	threshold.Init()
	n := len(operators)
	if n < 4 {
		return nil, fmt.Errorf("stresstest: BLSKeys needs at least n=4 operators (got %d)", n)
	}
	if (n-1)%3 != 0 {
		return nil, fmt.Errorf("stresstest: BLSKeys needs n = 3f+1 (got %d)", n)
	}
	f := (n - 1) / 3
	q := 2*f + 1

	master := &bls.SecretKey{}
	master.SetByCSPRNG()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n)) //nolint:gosec // small positive
	if err != nil {
		return nil, fmt.Errorf("stresstest: threshold.Create: %w", err)
	}

	out := &BLSKeys{
		ClusterPubKey: master.GetPublicKey().Serialize(),
		Shares:        make(map[obft.OperatorID][]byte, n),
		PubShares:     make(map[obft.OperatorID][]byte, n),
	}
	for _, op := range operators {
		sk, ok := shares[uint64(op)]
		if !ok {
			return nil, fmt.Errorf("stresstest: missing share for op %d", op)
		}
		out.Shares[op] = sk.Serialize()
		out.PubShares[op] = sk.GetPublicKey().Serialize()
	}
	return out, nil
}

// validate sanity-checks the config and fills defaults where appropriate.
func (c *SimConfig) validate() error {
	if c.N < 4 {
		return fmt.Errorf("stresstest: N must be >= 4 (n = 3f+1 minimum)")
	}
	if (c.N-1)%3 != 0 {
		return fmt.Errorf("stresstest: N must be 3f+1 (got %d)", c.N)
	}
	if c.K < 3 {
		return fmt.Errorf("stresstest: K must be >= 3 (OBFT minimum)")
	}
	if c.K > c.N {
		return fmt.Errorf("stresstest: K (%d) must be <= N (%d)", c.K, c.N)
	}
	if c.D <= 0 {
		return fmt.Errorf("stresstest: D must be > 0")
	}
	if c.TCommit <= 0 || c.Delta2 <= 0 || c.Delta3 <= 0 {
		return fmt.Errorf("stresstest: TCommit/Delta2/Delta3 must be > 0")
	}
	if c.Network == nil {
		c.Network = ConstantDelay{D: c.D}
	}
	if c.Byz == nil {
		c.Byz = ByzNone{}
	}
	if c.Host == nil {
		c.Host = HostAllValid{}
	}
	return nil
}

// NetworkModel decides how long a single message takes to propagate from
// `from` to `to`. The harness calls this once per (sender, receiver,
// message-kind) tuple at emission time and schedules the arrival event
// at `now + delay`.
type NetworkModel interface {
	Delay(rng *mrand.Rand, from, to obft.OperatorID, kind MsgKind) time.Duration
}

// ConstantDelay returns the same D for every message. Reproducible and
// matches BFT-comparison.md's "P99 worst-case D" framing.
type ConstantDelay struct{ D time.Duration }

func (c ConstantDelay) Delay(_ *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration {
	return c.D
}

// JitteredDelay draws a per-message delay from a uniform distribution on
// [D - Jitter, D + Jitter], clamped to >= 1ns. Useful for stress-testing
// scenarios whose outcome depends on per-message timing variation —
// healthy-path with realistic mesh jitter, byz-equivocation patterns where
// honest peers may receive different V's first depending on order, etc.
//
// Same (Seed, NetworkModel) → byte-identical event ordering, since the
// RNG draws happen on the single event-loop goroutine in scheduling order.
type JitteredDelay struct {
	D      time.Duration
	Jitter time.Duration
}

func (j JitteredDelay) Delay(rng *mrand.Rand, _, _ obft.OperatorID, _ MsgKind) time.Duration {
	if j.Jitter <= 0 {
		return j.D
	}
	// rng.Int63n(2*j.Jitter+1) is uniform on [0, 2*Jitter]; subtract Jitter
	// for [-Jitter, +Jitter].
	delta := time.Duration(rng.Int63n(int64(2*j.Jitter+1))) - j.Jitter
	d := j.D + delta
	if d < time.Nanosecond {
		d = time.Nanosecond
	}
	return d
}

// MsgKind discriminates the OBFT envelope kinds for the network /
// byz models.
type MsgKind int

const (
	KindPhase1Bundle MsgKind = iota
	KindCommit
	KindCertificate
)

// HostPattern returns the host application's valid/not-valid verdict for
// (operator, layer, V). Called once per peer-bundle observation at the
// receiver.
type HostPattern interface {
	Validate(op obft.OperatorID, layer int, v obft.Value) bool
}

// HostAllValid is the trivial all-valid host. Used for network-failure
// tests where the validity axis isn't being exercised.
type HostAllValid struct{}

func (HostAllValid) Validate(_ obft.OperatorID, _ int, _ obft.Value) bool { return true }

// HostInvalidForOperators returns false for any (op, layer) where
// Operators[op]. Useful for validity-divergence scenarios.
type HostInvalidForOperators struct {
	Layer     int
	Operators map[obft.OperatorID]bool
}

func (h HostInvalidForOperators) Validate(op obft.OperatorID, layer int, _ obft.Value) bool {
	if layer != h.Layer {
		return true
	}
	return !h.Operators[op]
}

// Run executes one simulation and returns the outcome. The sim is fully
// deterministic given (cfg, seed) — running the same config twice yields
// byte-identical events and outcome.
func Run(cfg SimConfig) (Outcome, error) {
	if err := cfg.validate(); err != nil {
		return Outcome{}, err
	}

	s := newSim(cfg)
	if err := s.start(); err != nil {
		return Outcome{}, err
	}
	s.runLoop()
	return s.outcome(), nil
}

// MustRun is Run that panics on configuration errors. Convenient for
// table-driven tests.
func MustRun(cfg SimConfig) Outcome {
	out, err := Run(cfg)
	if err != nil {
		panic(err)
	}
	return out
}

// ---- internal simulator state -------------------------------------------

type sim struct {
	cfg          SimConfig
	rng          *mrand.Rand
	now          time.Duration
	queue        eventQueue
	seq          int64
	operators    []obft.OperatorID
	cfgObft      *obft.Config
	pubShares    map[obft.OperatorID][]byte
	clusterPub   []byte
	instances    map[obft.OperatorID]*obft.Instance
	resolved     map[obft.OperatorID]*obft.Output
	resolvedAt   map[obft.OperatorID]time.Duration
	resolveErrs  map[obft.OperatorID]error
	canonValues  map[int]obft.Value // canonical V per layer (used by ByzNone-style honest leaders)
	trace        []TraceEntry
}

func newSim(cfg SimConfig) *sim {
	N := cfg.N
	operators := make([]obft.OperatorID, N)
	for i := 0; i < N; i++ {
		operators[i] = obft.OperatorID(i + 1)
	}

	var pubShares map[obft.OperatorID][]byte
	var clusterPub []byte
	if cfg.BLSKeys != nil {
		pubShares = cfg.BLSKeys.PubShares
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
		cfg:          cfg,
		rng:          mrand.New(mrand.NewSource(cfg.Seed)),
		operators:    operators,
		pubShares:    pubShares,
		clusterPub:   clusterPub,
		canonValues:  canonValues,
		instances:    make(map[obft.OperatorID]*obft.Instance, N),
		resolved:     make(map[obft.OperatorID]*obft.Output, N),
		resolvedAt:   make(map[obft.OperatorID]time.Duration, N),
		resolveErrs:  make(map[obft.OperatorID]error, N),
	}
}

// start builds the OBFT Config + Instances and seeds the initial event set:
// per-layer leader-fetch events for honest leaders, T_commit, T_phase2_end,
// and T_round_end.
func (s *sim) start() error {
	K := s.cfg.K
	fetchAt := s.cfg.FetchAt
	if fetchAt == nil {
		fetchAt = defaultFetchSchedule(K, s.cfg.TCommit, s.cfg.D, s.cfg.Delta)
	}

	layers := make([]obft.LayerSpec, K)
	for k := 0; k < K; k++ {
		layers[k] = obft.LayerSpec{
			Leader:  s.operators[k%s.cfg.N],
			FetchAt: fetchAt[k],
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
		return fmt.Errorf("stresstest: invalid OBFT config: %w", err)
	}
	s.cfgObft = cfgObft

	q := s.cfg.N - (s.cfg.N-1)/3 // q = 2f+1
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
			share := s.cfg.BLSKeys.Shares[op]
			signer = blsbackend.New(share)
			tagSigner = blsbackend.NewKyberSigner(share)
		} else {
			stub := obft.NewStubSigner(q, []byte{byte(op)})
			signer = stub
			tagSigner = stub
		}
		inst, err := obft.NewInstance(cfgObft, op, signer, tagSigner, ibe, s.clusterPub, s.pubShares, nil)
		if err != nil {
			return fmt.Errorf("stresstest: new instance for op %d: %w", op, err)
		}
		s.instances[op] = inst
	}

	// Schedule per-layer leader fetch events. The byz pattern decides
	// whether the leader actually fetches/broadcasts (or fetches with
	// equivocation, etc.).
	for k := 0; k < K; k++ {
		s.schedule(fetchAt[k], &evtLeaderFetch{layer: k})
	}
	// Phase boundaries.
	s.schedule(cfgObft.TCommit, &evtPhaseTwoStart{})
	s.schedule(cfgObft.RoundEndOffset(), &evtResolve{})

	return nil
}

func (s *sim) runLoop() {
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(*queueItem)
		s.now = e.when
		if s.cfg.TraceEnabled {
			s.trace = append(s.trace, TraceEntry{
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

func (s *sim) outcome() Outcome {
	out := Outcome{
		Decided:     false,
		Layer:       -1,
		PerOp:       make(map[obft.OperatorID]OperatorOutcome, len(s.operators)),
		Evidence:    make(map[obft.EvidenceRule]int),
		Trace:       s.trace,
	}
	earliestT := time.Duration(-1)
	for _, op := range s.operators {
		o := OperatorOutcome{Decided: false, Layer: -1}
		if res := s.resolved[op]; res != nil {
			o.Decided = true
			o.Layer = res.Layer
			o.Value = append(obft.Value{}, res.Value...)
			o.Time = s.resolvedAt[op]
			if earliestT < 0 || o.Time < earliestT {
				earliestT = o.Time
				out.Decided = true
				out.Layer = res.Layer
				out.Value = append(obft.Value{}, res.Value...)
				out.DecisionTime = o.Time
			}
		}
		if err, ok := s.resolveErrs[op]; ok {
			o.Err = err.Error()
		}
		ev := s.instances[op].Evidence()
		o.EvidenceCount = len(ev)
		for _, e := range ev {
			out.Evidence[e.Rule]++
		}
		out.PerOp[op] = o
	}
	return out
}

// hashValue returns a stable hex prefix of sha256(v) for diagnostics.
func hashValue(v obft.Value) string {
	h := sha256.Sum256(v)
	return fmt.Sprintf("%x", h[:6])
}

// defaultFetchSchedule returns a strictly-decreasing per-layer FetchAt with
// T_0 closest to T_broadcast_max and T_{K-1} earliest.
func defaultFetchSchedule(K int, tCommit, d, delta time.Duration) []time.Duration {
	tBroadcastMax := tCommit - 2*(d+delta)
	out := make([]time.Duration, K)
	step := tBroadcastMax / time.Duration(K+1)
	for k := 0; k < K; k++ {
		// k=0 → tBroadcastMax - step (latest); k=K-1 → step (earliest).
		out[k] = tBroadcastMax - time.Duration(k+1)*step + step // = tBroadcastMax - k*step
		// Simpler: step back from tBroadcastMax by k*step.
		out[k] = tBroadcastMax - time.Duration(k)*step
	}
	return out
}

// honestLeaderValue returns the canonical V the layer-k honest leader
// would fetch.
func (s *sim) honestLeaderValue(layer int) obft.Value {
	return s.canonValues[layer]
}

// emitToAll schedules per-receiver arrival events for `data` from `from`
// to all peers (excluding `from`). Per-receiver delay is drawn from the
// network model; per-receiver overrides from the byz model can adjust or
// suppress the delivery.
func (s *sim) emitToAll(from obft.OperatorID, kind MsgKind, build func(to obft.OperatorID) event) {
	for _, to := range s.operators {
		if to == from {
			continue
		}
		if !s.cfg.Byz.AllowDelivery(from, to, kind) {
			continue
		}
		delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
		if delay < 0 {
			delay = s.cfg.Network.Delay(s.rng, from, to, kind)
		}
		ev := build(to)
		s.schedule(s.now+delay, ev)
	}
}

// emitTo schedules a per-receiver arrival event for `data` from `from` to
// `to` only (modeling byz selective-delivery).
func (s *sim) emitTo(from, to obft.OperatorID, kind MsgKind, ev event) {
	delay := s.cfg.Byz.OverrideDelay(s.rng, from, to, kind)
	if delay < 0 {
		delay = s.cfg.Network.Delay(s.rng, from, to, kind)
	}
	s.schedule(s.now+delay, ev)
}

// observedOffset returns the receiver acceptance window offset for an
// arrival at virtual time s.now. Internal helper.
func (s *sim) observedOffset() time.Duration { return s.now }

