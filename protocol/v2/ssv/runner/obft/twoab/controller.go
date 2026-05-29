package twoab

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Controller is the SSV-side state-machine wrapper around twoab.Instance —
// the 2abOBFT sibling of the bare-OBFT runner Controller. It owns per-cluster
// identity material and a slot-keyed map of currently-active 2abOBFT instances.
//
// The Controller is per (operator, cluster, role). Lifecycle (driven by the
// runner):
//
//	StartNewInstance(slot)               → RunningInstance
//	  ObservePhase1Bundle(b, observedAt) // own bundle and peers'
//	  ApplyHostValidity(layer, V, valid) // host's verdict on V
//	  BuildPhase1Bundle(slot, layer, V)  // when local op is the layer's leader
//	  FirePhase2a(slot)                  → one of Value / NoValue / Commit-NRDirect
//	  ProcessValueMsg / ProcessNoValueMsg / ProcessCommit / ProcessCertificate
//	  OwnValueMsg / OwnNoValueMsg / OwnCommit // detect cascade-fired emissions to broadcast
//	  Resolve(slot)                      → *Output (observer-mode, opportunistic)
//	  BuildCertificate(slot, out)        → *Certificate
//	EndInstance(slot)
//
// Unlike bare OBFT there is no scheduled commit-build step: Phase 2b is
// dynamic (cascade-driven inside the Instance), so the runner detects new own
// emissions via OwnValueMsg / OwnCommit after each Observe* / FirePhase2a and
// broadcasts them.
//
// Concurrency: all methods are safe to call from multiple goroutines (the
// instances map is mutex-protected; per-Instance access is serialized via the
// RunningInstance's lock).
type Controller struct {
	operatorID      spectypes.OperatorID
	committee       []spectypes.OperatorID
	clusterID       [32]byte
	clusterPubKey   []byte
	pubKeyShares    map[twoabcore.OperatorID][]byte
	ibePubKeyShares map[twoabcore.OperatorID][]byte // optional under Option A

	signer    twoabcore.Signer
	tagSigner twoabcore.Signer // may be nil → falls back to signer in Instance ctor
	ibe       twoabcore.ThresholdIBE
	overrides *ConfigOverrides

	// evidenceObserver is registered on every Instance the Controller
	// creates. Optional; nil disables. Set via ControllerOptions.
	evidenceObserver twoabcore.EvidenceObserver

	mu        sync.Mutex
	instances map[phase0.Slot]*RunningInstance

	// pending buffers envelopes that arrived before a slot's instance started;
	// ended fences recently torn-down slots against post-teardown re-buffering.
	// Both are plain (non-thread-safe) collections serialized under mu — see
	// slotbuffers.go.
	pending *pendingBuffer
	ended   *endedRing

	// onInstanceEnd, if non-nil, is invoked by EndInstance after the instance
	// has been sealed (Finalize) and detached from the map, with the now-final
	// RunningInstance. Test-only observation seam — nil in production, so it
	// carries zero overhead. The race-safety bridge sets it to deterministically
	// snapshot each instance's final resolve trace + cascade errors at teardown,
	// replacing a former 100µs polling capture goroutine that could miss
	// instances whose RunProposerSlot completed between ticks (a miss that
	// misclassified fast local-deciders as cert-gossip deciders — see the
	// bridge's reconstructOutcome).
	onInstanceEnd func(slot phase0.Slot, r *RunningInstance)
}

// ErrNoActiveInstance is returned by Controller.lookup when no instance exists
// for the requested slot. Callers (e.g. the dispatcher) use
// errors.Is(err, ErrNoActiveInstance) to buffer the envelope for replay rather
// than dropping it as an error.
var ErrNoActiveInstance = errors.New("twoab adapter: no active instance for slot")

// PendingEnvelope holds an envelope dispatch targeted at a slot whose instance
// hadn't yet started. Exactly one of the body fields is non-nil. Unlike bare
// OBFT it carries the Phase-2a kinds (ValueMsg / NoValueMsg) too.
type PendingEnvelope struct {
	Bundle         *twoabcore.Phase1Bundle
	ValueMsg       *twoabcore.ValueMsg
	NoValueMsg     *twoabcore.NoValueMsg
	Commit         *twoabcore.Commit
	Certificate    *twoabcore.Certificate
	ObservedOffset time.Duration
}

// RunningInstance bundles a started 2abOBFT instance with the metadata the
// runner needs to drive Phases 1 / 2a / 2b / 3.
type RunningInstance struct {
	Slot   phase0.Slot
	Config *twoabcore.Config

	// LeaderAtLayers lists the layer indices where the local operator is the
	// designated leader for this slot (for per-layer fetch scheduling).
	LeaderAtLayers []int

	instanceMu sync.Mutex
	instance   *twoabcore.Instance

	// stateDelta is a buffered (cap 1) signal channel fired after every
	// successful FirePhase2a / ObserveValueMsg / ObserveNoValueMsg /
	// ObserveCommit / ObserveCertificate. The Scheduler's opportunistic
	// resolve+broadcast loop blocks on it and, on each delta, re-checks for
	// new own emissions to broadcast and retries Resolve. Closed by
	// EndInstance under instanceMu after Finalize seals the instance, so any
	// concurrent sender that acquired instanceMu before EndInstance has
	// already returned (and any sender that acquires it after sees
	// Ended()==true and skips the send).
	stateDelta chan struct{}
}

// ControllerOptions parameterizes Controller construction.
type ControllerOptions struct {
	OperatorID      spectypes.OperatorID
	Committee       []spectypes.OperatorID
	ClusterID       [32]byte
	ClusterPubKey   []byte
	PubKeyShares    map[twoabcore.OperatorID][]byte
	IBEPubKeyShares map[twoabcore.OperatorID][]byte // optional (Option A leaves nil)

	Signer    twoabcore.Signer
	TagSigner twoabcore.Signer // optional; falls back to Signer
	IBE       twoabcore.ThresholdIBE

	Overrides *ConfigOverrides

	// EvidenceObserver, if non-nil, is registered on every per-slot Instance
	// the Controller creates (honest operators log observed slashing evidence
	// for out-of-band aggregation).
	EvidenceObserver twoabcore.EvidenceObserver
}

// NewController constructs a Controller from the given options.
func NewController(opts ControllerOptions) (*Controller, error) {
	if opts.Signer == nil {
		return nil, errors.New("twoab adapter: nil Signer")
	}
	if opts.IBE == nil {
		return nil, errors.New("twoab adapter: nil IBE")
	}
	if opts.PubKeyShares == nil {
		return nil, errors.New("twoab adapter: nil PubKeyShares")
	}
	if len(opts.Committee) == 0 {
		return nil, errors.New("twoab adapter: empty committee")
	}

	committee := make([]spectypes.OperatorID, len(opts.Committee))
	copy(committee, opts.Committee)

	return &Controller{
		operatorID:       opts.OperatorID,
		committee:        committee,
		clusterID:        opts.ClusterID,
		clusterPubKey:    opts.ClusterPubKey,
		pubKeyShares:     opts.PubKeyShares,
		ibePubKeyShares:  opts.IBEPubKeyShares,
		signer:           opts.Signer,
		tagSigner:        opts.TagSigner,
		ibe:              opts.IBE,
		overrides:        opts.Overrides,
		evidenceObserver: opts.EvidenceObserver,
		instances:        make(map[phase0.Slot]*RunningInstance),
		pending:          newPendingBuffer(),
		ended:            newEndedRing(),
	}, nil
}

// BufferEnvelope queues an envelope for a slot whose instance hasn't yet
// started. Drops it if the slot was recently ended, the per-slot buffer is
// full, or MaxPendingSlots is exceeded (oldest entry evicted FIFO).
func (c *Controller) BufferEnvelope(slot phase0.Slot, env PendingEnvelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended.has(slot) {
		return
	}
	c.pending.add(slot, env)
}

// DrainPending removes and returns all buffered envelopes for `slot`, replayed
// into the now-active instance after StartNewInstance.
func (c *Controller) DrainPending(slot phase0.Slot) []PendingEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending.drain(slot)
}

// ForgetPending evicts buffered envelopes for `slot` without dispatch.
func (c *Controller) ForgetPending(slot phase0.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending.forget(slot)
}

// StartNewInstance initializes a 2abOBFT instance for the given slot and
// records it in the active-instances map.
func (c *Controller) StartNewInstance(slot phase0.Slot) (*RunningInstance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.instances[slot]; exists {
		return nil, fmt.Errorf("twoab adapter: instance already running for slot %d", slot)
	}

	cfg, err := ConfigForCluster(slot, c.committee, c.clusterID, c.overrides)
	if err != nil {
		return nil, fmt.Errorf("twoab adapter: build config: %w", err)
	}

	inst, err := twoabcore.NewInstance(
		cfg, twoabcore.OperatorID(c.operatorID),
		c.signer, c.tagSigner, c.ibe,
		c.clusterPubKey, c.pubKeyShares, c.ibePubKeyShares,
		c.evidenceObserver,
	)
	if err != nil {
		return nil, fmt.Errorf("twoab adapter: new instance: %w", err)
	}

	leaderAt := computeLeaderLayers(cfg, c.operatorID)
	r := &RunningInstance{
		Slot:           slot,
		Config:         cfg,
		LeaderAtLayers: leaderAt,
		instance:       inst,
		stateDelta:     make(chan struct{}, 1),
	}
	c.instances[slot] = r
	return r, nil
}

// GetInstance returns the running instance for `slot`, or nil if none.
func (c *Controller) GetInstance(slot phase0.Slot) (*RunningInstance, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	return r, ok
}

// OperatorID returns the local operator's ID.
func (c *Controller) OperatorID() spectypes.OperatorID {
	return c.operatorID
}

// EndInstance seals the running instance (Finalize sets the ended flag), then
// removes it from the controller's tracking map. Idempotent. Finally fires the
// onInstanceEnd observation hook (if set) with the detached instance.
//
// Race-fence: a goroutine that captured the instance via lookup() but had not
// yet acquired r.instanceMu can still race past EndInstance. Finalize sets the
// ended flag under r.instanceMu; every state-touching Controller method
// re-checks it after acquiring r.instanceMu and returns ErrNoActiveInstance if
// set, so no state mutation runs after Finalize.
func (c *Controller) EndInstance(slot phase0.Slot) {
	c.mu.Lock()
	r, ok := c.instances[slot]
	if !ok {
		c.mu.Unlock()
		return
	}
	r.instanceMu.Lock()
	r.instance.Finalize()
	// Close the state-delta channel under instanceMu; mutators re-check
	// Ended() under instanceMu before sending, so no send-on-closed race.
	close(r.stateDelta)
	r.instanceMu.Unlock()
	delete(c.instances, slot)
	c.pending.forget(slot)
	c.ended.mark(slot)
	hook := c.onInstanceEnd
	c.mu.Unlock()

	// Fire the observation hook AFTER releasing c.mu so it may re-lock
	// r.instanceMu to snapshot now-final state without holding the controller
	// lock (lookup→instanceMu is the only lock order in this type, so a hook
	// taking instanceMu under c.mu wouldn't deadlock either — firing it outside
	// just keeps the teardown lock-hold minimal). r is detached from the map so
	// no other caller can reach it, but the pointer stays valid for the read;
	// Finalize has set the ended flag, so any still-draining mutator is now a
	// no-op and the hook reads a consistent final trace. Nil in production.
	if hook != nil {
		hook(slot, r)
	}
}

// ActiveSlots returns the slots with currently-running instances, sorted.
func (c *Controller) ActiveSlots() []phase0.Slot {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]phase0.Slot, 0, len(c.instances))
	for s := range c.instances {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// BuildPhase1Bundle has the local operator (as the layer's leader) construct a
// Phase-1 bundle for `value`.
func (c *Controller) BuildPhase1Bundle(slot phase0.Slot, layer int, value []byte) (*twoabcore.Phase1Bundle, error) {
	return withLiveInstance(c, slot, func(r *RunningInstance) (*twoabcore.Phase1Bundle, error) {
		return r.instance.BuildPhase1Bundle(layer, twoabcore.Value(value))
	})
}

// ObservePhase1Bundle records a peer's (or own) Phase-1 bundle. `observedOffset`
// is the bundle's first-observation time relative to slot start.
func (c *Controller) ObservePhase1Bundle(b *twoabcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("twoab adapter: nil phase-1 bundle")
	}
	return withLiveInstanceErr(c, phase0.Slot(b.Height), func(r *RunningInstance) error {
		if err := r.instance.ObservePhase1Bundle(b, observedOffset); err != nil {
			return err
		}
		// A late bundle observed post-fire can cascade into an own emission
		// (e.g. enables σ for an op that fired NoValue → upgrade); wake the
		// resolve loop so it broadcasts any new emission and re-resolves.
		signalStateDeltaLocked(r)
		return nil
	})
}

// ApplyHostValidity records the host application's valid/not-valid verdict for
// `value` at `layer`. Called at multiple points (Phase-1 observe, Phase-2a
// fire, Phase-2b commit-eval) — the Instance dedups per (layer, V).
func (c *Controller) ApplyHostValidity(slot phase0.Slot, layer int, value []byte, valid bool) error {
	return withLiveInstanceErr(c, slot, func(r *RunningInstance) error {
		if err := r.instance.ApplyHostValidity(layer, twoabcore.Value(value), valid); err != nil {
			return err
		}
		// A host verdict can flip an op σ-eligible (NoValue → upgrade) via the
		// afterStateDelta cascade; wake the resolve loop to broadcast it.
		signalStateDeltaLocked(r)
		return nil
	})
}

// FirePhase2a fires the one-shot Phase-2a emission for the slot: the operator
// emits exactly one of KindValue / KindNoValue / KindCommit-NRDirect per its
// local state. On success exactly one of the three is non-nil; all three are
// nil only when the returned error is non-nil (no live instance, or a Phase-2a
// build failure). The caller broadcasts the returned message and then polls
// OwnValueMsg / OwnCommit for any cascade-fired follow-ups.
func (c *Controller) FirePhase2a(slot phase0.Slot) (*twoabcore.ValueMsg, *twoabcore.NoValueMsg, *twoabcore.Commit, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, nil, nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return nil, nil, nil, ErrNoActiveInstance
	}
	v, nv, cm, err := r.instance.MaybeFirePhase2a()
	if err != nil {
		return nil, nil, nil, err
	}
	signalStateDeltaLocked(r)
	return v, nv, cm, nil
}

// ProcessValueMsg routes a peer's Phase-2a KindValue to the right instance.
func (c *Controller) ProcessValueMsg(v *twoabcore.ValueMsg) error {
	if v == nil {
		return errors.New("twoab adapter: nil value msg")
	}
	return withLiveInstanceErr(c, phase0.Slot(v.Height), func(r *RunningInstance) error {
		if err := r.instance.ObserveValueMsg(v); err != nil {
			return err
		}
		signalStateDeltaLocked(r)
		return nil
	})
}

// ProcessNoValueMsg routes a peer's Phase-2a KindNoValue to the right instance.
func (c *Controller) ProcessNoValueMsg(nv *twoabcore.NoValueMsg) error {
	if nv == nil {
		return errors.New("twoab adapter: nil no-value msg")
	}
	return withLiveInstanceErr(c, phase0.Slot(nv.Height), func(r *RunningInstance) error {
		if err := r.instance.ObserveNoValueMsg(nv); err != nil {
			return err
		}
		signalStateDeltaLocked(r)
		return nil
	})
}

// ProcessCommit routes a peer's KindCommit (Signed / NR / NRDirect — the Side
// flag is handled inside the Instance) to the right instance.
func (c *Controller) ProcessCommit(cm *twoabcore.Commit) error {
	if cm == nil {
		return errors.New("twoab adapter: nil commit")
	}
	return withLiveInstanceErr(c, phase0.Slot(cm.Height), func(r *RunningInstance) error {
		if err := r.instance.ObserveCommit(cm); err != nil {
			return err
		}
		signalStateDeltaLocked(r)
		return nil
	})
}

// ProcessCertificate routes a peer's Certificate to the right instance.
func (c *Controller) ProcessCertificate(cert *twoabcore.Certificate) error {
	if cert == nil {
		return errors.New("twoab adapter: nil certificate")
	}
	return withLiveInstanceErr(c, phase0.Slot(cert.Height), func(r *RunningInstance) error {
		if err := r.instance.ObserveCertificate(cert); err != nil {
			return err
		}
		signalStateDeltaLocked(r)
		return nil
	})
}

// OwnValueMsg returns the local operator's own KindValue emission for the slot
// (the σ-side terminal emission, possibly a cascade-fired Phase-2a-late
// upgrade), or nil if none has fired. The scheduler broadcasts newly-set
// emissions detected after each Observe* / FirePhase2a.
func (c *Controller) OwnValueMsg(slot phase0.Slot) (*twoabcore.ValueMsg, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*twoabcore.ValueMsg, error) {
		if msg, ok := r.instance.OwnValueMsg(); ok {
			return msg, nil
		}
		return nil, nil
	})
}

// OwnNoValueMsg returns the local operator's own KindNoValue emission, or nil.
func (c *Controller) OwnNoValueMsg(slot phase0.Slot) (*twoabcore.NoValueMsg, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*twoabcore.NoValueMsg, error) {
		if msg, ok := r.instance.OwnNoValueMsg(); ok {
			return msg, nil
		}
		return nil, nil
	})
}

// OwnCommit returns the local operator's own KindCommit emission (a cascade-
// fired Phase-2b commit), or nil if none has fired yet.
func (c *Controller) OwnCommit(slot phase0.Slot) (*twoabcore.Commit, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*twoabcore.Commit, error) {
		if msg, ok := r.instance.OwnCommit(); ok {
			return msg, nil
		}
		return nil, nil
	})
}

// signalStateDeltaLocked publishes a non-blocking signal on r.stateDelta to
// wake the Scheduler's opportunistic loop. The channel is buffered (cap 1) so
// follow-up deltas coalesce — the waiter re-reads instance state on each
// receive. Caller must hold r.instanceMu (so the channel is not yet closed).
func signalStateDeltaLocked(r *RunningInstance) {
	select {
	case r.stateDelta <- struct{}{}:
	default:
	}
}

// StateDeltaChan returns the per-slot state-delta signal channel. If no
// instance is running for `slot`, returns a pre-closed channel.
func (c *Controller) StateDeltaChan(slot phase0.Slot) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	if !ok {
		return closedStructChan
	}
	return r.stateDelta
}

// L0ReadyCh returns the slot's instance L_0-ready channel (closed once the
// operator's L_0 emission is determinable — the early-fire trigger for
// FirePhase2a). Returns a pre-closed channel if the slot has no active instance.
func (c *Controller) L0ReadyCh(slot phase0.Slot) <-chan struct{} {
	return liveInstanceChan(c, slot, func(r *RunningInstance) <-chan struct{} {
		return r.instance.L0ReadyCh()
	}, closedStructChan)
}

// WantsHostValidationCh returns the slot's instance host-validation request
// channel. The runner drains it, dispatches HostValidate, and calls back via
// ApplyHostValidity. Returns a closed channel if the slot has no active instance.
func (c *Controller) WantsHostValidationCh(slot phase0.Slot) <-chan twoabcore.ValidationRequest {
	return liveInstanceChan(c, slot, func(r *RunningInstance) <-chan twoabcore.ValidationRequest {
		return r.instance.WantsHostValidationCh()
	}, closedValidationReqChan)
}

// Resolve runs the Phase-3 reconstruction walk (observer-mode; called
// opportunistically on each state delta) and returns the decided Output or an
// error (typically twoab.ErrNoQuorum if the slot was missed).
func (c *Controller) Resolve(slot phase0.Slot) (*twoabcore.Output, error) {
	return withLiveInstance(c, slot, func(r *RunningInstance) (*twoabcore.Output, error) {
		return r.instance.Resolve()
	})
}

// BuildCertificate produces a final-certificate gossip message for an
// already-resolved Output.
func (c *Controller) BuildCertificate(slot phase0.Slot, out *twoabcore.Output) (*twoabcore.Certificate, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*twoabcore.Certificate, error) {
		return r.instance.BuildCertificate(out)
	})
}

// RetainedCertificate returns a peer-broadcast certificate (if any) — usable
// as an alternative submission path when local Resolve fails.
func (c *Controller) RetainedCertificate(slot phase0.Slot) (*twoabcore.Certificate, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*twoabcore.Certificate, error) {
		return r.instance.RetainedCertificate(), nil
	})
}

// Evidence returns the slashing-evidence accumulated on this instance.
func (c *Controller) Evidence(slot phase0.Slot) ([]twoabcore.Evidence, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) ([]twoabcore.Evidence, error) {
		return r.instance.Evidence(), nil
	})
}

func (c *Controller) lookup(slot phase0.Slot) (*RunningInstance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	if !ok {
		return nil, fmt.Errorf("%w %d", ErrNoActiveInstance, slot)
	}
	return r, nil
}

// withLiveInstance looks up slot's instance, takes its per-instance lock,
// verifies it has not been Finalize'd, and runs fn under that lock — the single
// enforcement point for the lookup → lock → ended-check contract every
// state-touching method shares. Returns ErrNoActiveInstance if the slot has no
// instance or it has ended. (Duplicated from the bare-OBFT runner per the
// separate-runner approach; the Controller type differs.)
func withLiveInstance[T any](c *Controller, slot phase0.Slot, fn func(r *RunningInstance) (T, error)) (T, error) {
	var zero T
	r, err := c.lookup(slot)
	if err != nil {
		return zero, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return zero, ErrNoActiveInstance
	}
	return fn(r)
}

// withLiveInstanceErr is withLiveInstance for methods that return only an error.
func withLiveInstanceErr(c *Controller, slot phase0.Slot, fn func(r *RunningInstance) error) error {
	_, err := withLiveInstance(c, slot, func(r *RunningInstance) (struct{}, error) {
		return struct{}{}, fn(r)
	})
	return err
}

// withInstanceForRead is like withLiveInstance but does NOT reject a Finalize'd
// instance — read-only accessors (OwnX / BuildCertificate / RetainedCertificate
// / Evidence) are intentionally callable in the Finalize→EndInstance window.
func withInstanceForRead[T any](c *Controller, slot phase0.Slot, fn func(r *RunningInstance) (T, error)) (T, error) {
	var zero T
	r, err := c.lookup(slot)
	if err != nil {
		return zero, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return fn(r)
}

// liveInstanceChan returns the channel fn produces for slot's live instance,
// or a pre-closed channel if the slot has no active (non-ended) instance.
func liveInstanceChan[T any](c *Controller, slot phase0.Slot, fn func(r *RunningInstance) <-chan T, closedFallback <-chan T) <-chan T {
	r, err := c.lookup(slot)
	if err != nil {
		return closedFallback
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return closedFallback
	}
	return fn(r)
}

// closedChan returns an already-closed channel of T. Used only to build the
// package-level pre-closed channels below — not per-call (audit F14).
func closedChan[T any]() <-chan T {
	ch := make(chan T)
	close(ch)
	return ch
}

// Pre-built closed channels for the dead-instance accessor fallback paths —
// built once instead of make+close per call. Mirror of the bare-OBFT
// controller's vars (audit F14); receiving on a closed channel is idempotent
// so a single shared instance per concrete T is safe to reuse.
var (
	closedStructChan        = closedChan[struct{}]()
	closedValidationReqChan = closedChan[twoabcore.ValidationRequest]()
)

func computeLeaderLayers(cfg *twoabcore.Config, operatorID spectypes.OperatorID) []int {
	var layers []int
	for i, layer := range cfg.Layers {
		if layer.Leader == twoabcore.OperatorID(operatorID) {
			layers = append(layers, i)
		}
	}
	return layers
}
