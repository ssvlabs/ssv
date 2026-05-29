package obft

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Controller is the SSV-side state-machine wrapper around obft.Instance.
// It owns per-cluster identity material (operator share, cluster pubkey,
// pubkey-shares map) and a slot-keyed map of currently-active OBFT instances.
//
// The Controller is per (operator, cluster, role). Lifecycle (driven by the
// runner):
//
//	StartNewInstance(slot)               → RunningInstance
//	  ObservePhase1Bundle(b, observedAt) // for own bundle and peers'
//	  ApplyHostValidity(layer, V, valid) // host's verdict on V
//	  ProcessCommit(c)                   // peer's KindCommit at T_commit
//	  ProcessCertificate(c)              // optional alt-submission path
//	  BuildPhase1Bundle(slot, layer, V)  // when local op is the layer's leader
//	  BuildOwnCommit(slot)               // single emission at T_commit
//	  Resolve(slot)                      → *Output, error
//	  BuildCertificate(slot, out)        → *Certificate
//	EndInstance(slot)
//
// Concurrency: all methods are safe to call from multiple goroutines (the
// instances map is mutex-protected; per-Instance access is serialized via
// the RunningInstance's lock).
type Controller struct {
	operatorID      spectypes.OperatorID
	committee       []spectypes.OperatorID
	clusterID       [32]byte
	clusterPubKey   []byte
	pubKeyShares    map[obftcore.OperatorID][]byte
	ibePubKeyShares map[obftcore.OperatorID][]byte // optional under Option A

	signer    obftcore.Signer
	tagSigner obftcore.Signer // may be nil → falls back to signer in Instance ctor
	ibe       obftcore.ThresholdIBE
	overrides *ConfigOverrides

	// evidenceObserver is registered on every Instance the Controller
	// creates. Optional; nil disables. Set via ControllerOptions.
	evidenceObserver obftcore.EvidenceObserver

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
	// snapshot each instance's final resolve trace at teardown, replacing a
	// former 100µs polling capture goroutine that could miss instances whose
	// RunProposerSlot completed between ticks (a miss that misclassified fast
	// local-deciders as cert-gossip deciders — see the bridge's
	// reconstructOutcome).
	onInstanceEnd func(slot phase0.Slot, r *RunningInstance)
}

// ErrNoActiveInstance is returned by Controller.lookup when no instance
// exists for the requested slot. Callers (e.g. the dispatcher) use
// errors.Is(err, ErrNoActiveInstance) to detect this case and buffer the
// envelope for replay rather than dropping it as an error.
var ErrNoActiveInstance = errors.New("obft adapter: no active instance for slot")

// PendingEnvelope holds an envelope dispatch targeted at a slot whose
// instance hadn't yet started. Exactly one of the body fields is non-nil.
type PendingEnvelope struct {
	Bundle         *obftcore.Phase1Bundle
	Commit         *obftcore.Commit
	Certificate    *obftcore.Certificate
	ObservedOffset time.Duration
}

// RunningInstance bundles a started OBFT instance with the metadata the
// runner needs to drive Phases 1 / 2 / 3.
type RunningInstance struct {
	Slot   phase0.Slot
	Config *obftcore.Config

	// LeaderAtLayers lists the layer indices where the local operator is
	// the designated leader for this slot (for the runner's per-layer
	// fetch scheduling).
	LeaderAtLayers []int

	instanceMu sync.Mutex
	instance   *obftcore.Instance

	// stateDelta is a buffered (cap 1) signal channel fired after every
	// successful ObserveCommit / ObserveCertificate. The Scheduler's
	// ResolveAndSubmitOpportunistically blocks on receives from this
	// channel and retries Resolve on each delta. Closed by EndInstance
	// under instanceMu after Finalize seals the instance, so any
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
	PubKeyShares    map[obftcore.OperatorID][]byte
	IBEPubKeyShares map[obftcore.OperatorID][]byte // optional (Option A leaves nil)

	Signer    obftcore.Signer
	TagSigner obftcore.Signer // optional; falls back to Signer
	IBE       obftcore.ThresholdIBE

	Overrides *ConfigOverrides

	// EvidenceObserver, if non-nil, is registered on every per-slot
	// Instance the Controller creates. Per spec §Slashing evidence,
	// honest operators MUST log observed evidence for out-of-band
	// aggregation (manual-blacklist mechanism is the canonical consumer);
	// log format is implementation-defined. The SSV adapter wires this
	// to a zap WARN logger — see makeOBFTEvidenceObserver in
	// operator/validator/setup_obft.go.
	EvidenceObserver obftcore.EvidenceObserver
}

// NewController constructs a Controller from the given options.
func NewController(opts ControllerOptions) (*Controller, error) {
	if opts.Signer == nil {
		return nil, errors.New("obft adapter: nil Signer")
	}
	if opts.IBE == nil {
		return nil, errors.New("obft adapter: nil IBE")
	}
	if opts.PubKeyShares == nil {
		return nil, errors.New("obft adapter: nil PubKeyShares")
	}
	if len(opts.Committee) == 0 {
		return nil, errors.New("obft adapter: empty committee")
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
// started. Called by the dispatcher when Controller.lookup returns
// ErrNoActiveInstance. Drops the envelope if:
//   - the slot was recently EndInstance'd (post-teardown re-buffering would
//     just sit until LRU eviction since no instance can drain it);
//   - the per-slot buffer is full;
//   - or MaxPendingSlots is exceeded (oldest entry evicted FIFO).
//
// O(1) amortized: list.PushBack / list.Front+Remove.
func (c *Controller) BufferEnvelope(slot phase0.Slot, env PendingEnvelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Race-fence: if the slot has been ended, refuse to re-buffer. Closes
	// the window where a peer envelope arrives between EndInstance (which
	// also clears pending) and the slot-window check eventually rejecting
	// new arrivals at the validation layer.
	if c.ended.has(slot) {
		return
	}
	c.pending.add(slot, env)
}

// DrainPending removes and returns all buffered envelopes for `slot`. Called
// by RunProposerSlot after StartNewInstance so messages that arrived before
// the local instance started are replayed into the now-active instance.
func (c *Controller) DrainPending(slot phase0.Slot) []PendingEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending.drain(slot)
}

// ForgetPending evicts buffered envelopes for `slot` without dispatch. Used
// when the slot was never started and the buffer should be cleaned up to
// bound memory.
func (c *Controller) ForgetPending(slot phase0.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending.forget(slot)
}

// StartNewInstance initializes an OBFT instance for the given slot and
// records it in the active-instances map.
func (c *Controller) StartNewInstance(slot phase0.Slot) (*RunningInstance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.instances[slot]; exists {
		return nil, fmt.Errorf("obft adapter: instance already running for slot %d", slot)
	}

	cfg, err := ConfigForCluster(slot, c.committee, c.clusterID, c.overrides)
	if err != nil {
		return nil, fmt.Errorf("obft adapter: build config: %w", err)
	}

	inst, err := obftcore.NewInstance(
		cfg, obftcore.OperatorID(c.operatorID),
		c.signer, c.tagSigner, c.ibe,
		c.clusterPubKey, c.pubKeyShares, c.ibePubKeyShares,
		c.evidenceObserver,
	)
	if err != nil {
		return nil, fmt.Errorf("obft adapter: new instance: %w", err)
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

// OperatorID returns the local operator's ID.
func (c *Controller) OperatorID() spectypes.OperatorID {
	return c.operatorID
}

// EndInstance seals the running instance (Finalize sets the ended flag),
// then removes it from the controller's tracking map. Idempotent. Finally
// fires the onInstanceEnd observation hook (if set) with the detached instance.
//
// Race-fence: a goroutine that had already captured the instance pointer
// via lookup() but had not yet acquired r.instanceMu can still race past
// EndInstance. Finalize sets the Instance.ended flag under r.instanceMu;
// every Controller method re-checks that flag after acquiring r.instanceMu
// and returns ErrNoActiveInstance if set. This pair guarantees no state
// mutation can run after Finalize, even if the controller mutex was
// released between lookup and method dispatch.
func (c *Controller) EndInstance(slot phase0.Slot) {
	c.mu.Lock()
	r, ok := c.instances[slot]
	if !ok {
		c.mu.Unlock()
		return
	}
	r.instanceMu.Lock()
	r.instance.Finalize()
	// Close the state-delta channel under instanceMu. Mutators (ProcessCommit /
	// ProcessCertificate) re-check Ended() under instanceMu before sending,
	// so any goroutine that acquires instanceMu after this point sees the
	// finalized instance and skips the send — no send-on-closed-channel race.
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

// BuildPhase1Bundle has the local operator (as the layer's leader) construct
// a Phase-1 bundle for `value`. The bundle is also self-observed so the
// leader's σ_V is in the σ-pool at Resolve time.
func (c *Controller) BuildPhase1Bundle(slot phase0.Slot, layer int, value []byte) (*obftcore.Phase1Bundle, error) {
	return withLiveInstance(c, slot, func(r *RunningInstance) (*obftcore.Phase1Bundle, error) {
		return r.instance.BuildPhase1Bundle(layer, obftcore.Value(value))
	})
}

// ObservePhase1Bundle records a peer's (or own) Phase-1 bundle.
// `observedOffset` is the bundle's first-observation time relative to slot
// start. Bundles past T_accept_max are rejected with ErrLatePhase1Bundle.
func (c *Controller) ObservePhase1Bundle(b *obftcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("obft adapter: nil phase-1 bundle")
	}
	return withLiveInstanceErr(c, phase0.Slot(b.Height), func(r *RunningInstance) error {
		return r.instance.ObservePhase1Bundle(b, observedOffset)
	})
}

// ApplyHostValidity records the host application's valid/not-valid verdict
// for `value` at `layer`.
func (c *Controller) ApplyHostValidity(slot phase0.Slot, layer int, value []byte, valid bool) error {
	return withLiveInstanceErr(c, slot, func(r *RunningInstance) error {
		return r.instance.ApplyHostValidity(layer, obftcore.Value(value), valid)
	})
}

// ProcessCommit routes a peer's Commit to the right instance.
func (c *Controller) ProcessCommit(cm *obftcore.Commit) error {
	if cm == nil {
		return errors.New("obft adapter: nil commit")
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
func (c *Controller) ProcessCertificate(cert *obftcore.Certificate) error {
	if cert == nil {
		return errors.New("obft adapter: nil certificate")
	}
	return withLiveInstanceErr(c, phase0.Slot(cert.Height), func(r *RunningInstance) error {
		if err := r.instance.ObserveCertificate(cert); err != nil {
			return err
		}
		signalStateDeltaLocked(r)
		return nil
	})
}

// signalStateDeltaLocked publishes a non-blocking signal on r.stateDelta to
// wake the Scheduler's opportunistic-Resolve waiter. The channel is buffered
// (cap 1) so a pending unread signal coalesces follow-up deltas — the waiter
// re-reads instance state on each receive, so coalescing loses no
// information. Caller must hold r.instanceMu; this guarantees the channel
// is not yet closed (EndInstance closes under the same lock).
func signalStateDeltaLocked(r *RunningInstance) {
	select {
	case r.stateDelta <- struct{}{}:
	default:
	}
}

// StateDeltaChan returns the per-slot state-delta signal channel for the
// running instance. The Scheduler's ResolveAndSubmitOpportunistically uses
// this in place of a fixed-cadence ticker — see scheduler.go.
//
// If no instance is running for `slot`, returns a pre-closed channel so the
// caller's receive returns immediately; the caller will then exit via a
// subsequent Resolve(slot) returning ErrNoActiveInstance.
func (c *Controller) StateDeltaChan(slot phase0.Slot) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	if !ok {
		return closedChan[struct{}]()
	}
	return r.stateDelta
}

// BuildOwnCommit builds the local operator's KindCommit. Single emission per
// slot per spec §Phase 2. Per spec §Phase 2 emission-timing, the caller fires
// this at T_emit = min(L_0-observed-and-validated event, T_commit fallback);
// see L0ReadyCh for the early-emit trigger.
func (c *Controller) BuildOwnCommit(slot phase0.Slot) (*obftcore.Commit, error) {
	return withLiveInstance(c, slot, func(r *RunningInstance) (*obftcore.Commit, error) {
		return r.instance.BuildOwnCommit()
	})
}

// L0ReadyCh returns the slot's instance L_0-ready channel (closed when the
// operator has enough information at L_0 to commit early per spec §Phase 2
// emission-timing). Callers select on this against the T_commit fallback to
// decide when to invoke BuildOwnCommit. Returns a pre-closed channel if the
// slot has no active instance (so callers don't block forever on a stale
// slot).
func (c *Controller) L0ReadyCh(slot phase0.Slot) <-chan struct{} {
	return liveInstanceChan(c, slot, func(r *RunningInstance) <-chan struct{} {
		return r.instance.L0ReadyCh()
	})
}

// WantsHostValidationCh returns the slot's instance host-validation
// request channel. Per spec §Phase 2 / Peer-reflood V via early commit:
// the Instance enqueues (layer, V) pairs when V is first-observed via a
// peer's σ-onion entry without an existing host verdict. The runner is
// expected to drain this channel in a dedicated goroutine, dispatch
// HostValidate against the cluster's host hook, and call back through
// ApplyHostValidity with the verdict.
//
// Returns a closed channel if the slot has no active instance (so a
// runner-side goroutine exits cleanly via the channel-close branch).
func (c *Controller) WantsHostValidationCh(slot phase0.Slot) <-chan obftcore.ValidationRequest {
	return liveInstanceChan(c, slot, func(r *RunningInstance) <-chan obftcore.ValidationRequest {
		return r.instance.WantsHostValidationCh()
	})
}

// Resolve runs the Phase-3 reconstruction walk and returns the decided
// Output (value + full reconstructed signature) or an error
// (typically obftcore.ErrNoQuorum if the slot was missed).
func (c *Controller) Resolve(slot phase0.Slot) (*obftcore.Output, error) {
	return withLiveInstance(c, slot, func(r *RunningInstance) (*obftcore.Output, error) {
		return r.instance.Resolve()
	})
}

// BuildCertificate produces a final-certificate gossip message for an
// already-resolved Output.
func (c *Controller) BuildCertificate(slot phase0.Slot, out *obftcore.Output) (*obftcore.Certificate, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*obftcore.Certificate, error) {
		return r.instance.BuildCertificate(out)
	})
}

// RetainedCertificate returns a peer-broadcast certificate (if any) — usable
// as an alternative submission path when local Resolve fails.
func (c *Controller) RetainedCertificate(slot phase0.Slot) (*obftcore.Certificate, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) (*obftcore.Certificate, error) {
		return r.instance.RetainedCertificate(), nil
	})
}

// Evidence returns the slashing-evidence accumulated on this instance.
func (c *Controller) Evidence(slot phase0.Slot) ([]obftcore.Evidence, error) {
	return withInstanceForRead(c, slot, func(r *RunningInstance) ([]obftcore.Evidence, error) {
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
// state-touching Controller method shares. Returns ErrNoActiveInstance if the
// slot has no instance or it has ended.
//
// The ended re-check under instanceMu is load-bearing: a goroutine that
// captured the instance via lookup() before EndInstance ran could otherwise
// mutate a finalized instance, silently losing late evidence (e.g. a Rule 5
// candidate observed after Finalize set the ended flag). EndInstance sets that
// flag under instanceMu, so re-checking it here closes the race.
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
// instance. Read-only accessors (BuildCertificate / RetainedCertificate /
// Evidence) are intentionally callable on an instance that has Finalize'd
// (Ended() == true) but is still registered — the window between Finalize and
// EndInstance — e.g. to build a certificate from a cached Output, or read
// accumulated evidence for logging. (After EndInstance the slot is removed from
// c.instances, so lookup fails for these too.)
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
// or a pre-closed channel if the slot has no active (non-ended) instance — so
// callers select/range without blocking on a stale slot.
func liveInstanceChan[T any](c *Controller, slot phase0.Slot, fn func(r *RunningInstance) <-chan T) <-chan T {
	r, err := c.lookup(slot)
	if err != nil {
		return closedChan[T]()
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return closedChan[T]()
	}
	return fn(r)
}

// closedChan returns an already-closed channel of T (a receive returns the
// zero value immediately).
func closedChan[T any]() <-chan T {
	ch := make(chan T)
	close(ch)
	return ch
}

func computeLeaderLayers(cfg *obftcore.Config, operatorID spectypes.OperatorID) []int {
	var layers []int
	for i, layer := range cfg.Layers {
		if layer.Leader == obftcore.OperatorID(operatorID) {
			layers = append(layers, i)
		}
	}
	return layers
}
