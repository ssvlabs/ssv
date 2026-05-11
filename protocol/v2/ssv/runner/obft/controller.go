package obft

import (
	"container/list"
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

	// pending buffers envelopes that arrived before the slot's instance
	// started (e.g., faster peers send during the slow operator's pre-
	// consensus). Drained by Scheduler.DrainPending after StartNewInstance.
	// Capped per-slot to MaxPendingPerSlot and globally to MaxPendingSlots
	// (FIFO eviction via pendingOrder) to bound memory under abuse.
	pending      map[phase0.Slot][]PendingEnvelope
	pendingOrder *list.List                    // *list.Element holds phase0.Slot
	pendingElem  map[phase0.Slot]*list.Element // O(1) lookup for Drain/Forget

	// endedSlots is a small set of slots whose instance has been ended via
	// EndInstance. BufferEnvelope refuses to buffer for slots in this set,
	// closing the race where a late peer broadcast (post-EndInstance,
	// post-ForgetPending) would otherwise re-buffer an envelope that has
	// nowhere to drain into. Eviction is FIFO at MaxEndedSlots; slots that
	// fall out of the ring can re-accept pending entries (which then sit
	// until LRU eviction at MaxPendingSlots), but the ring is sized
	// generously enough to cover the validation slot-window so that's
	// effectively dead code.
	endedSlots     map[phase0.Slot]struct{}
	endedSlotOrder *list.List // *list.Element holds phase0.Slot, FIFO
}

// MaxEndedSlots caps how many recently-ended slots the controller remembers
// for the BufferEnvelope post-teardown gate. Sized to comfortably exceed
// the validation slot-window (obftAllowedPastSlots + obftAllowedFutureSlots
// ≈ 6) so any envelope that could pass slot-window admission is checked
// against the ended-set.
const MaxEndedSlots = 64

// MaxPendingPerSlot caps the number of envelopes buffered per slot before
// the instance starts. Above this, additional envelopes are dropped.
const MaxPendingPerSlot = 64

// MaxPendingSlots caps the total number of distinct slots with non-empty
// buffers. Bounds memory under an attacker that emits envelopes targeting
// many distinct slot numbers. When exceeded, the oldest slot entry is
// evicted (LRU).
const MaxPendingSlots = 256

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
		pending:          make(map[phase0.Slot][]PendingEnvelope),
		pendingOrder:     list.New(),
		pendingElem:      make(map[phase0.Slot]*list.Element),
		endedSlots:       make(map[phase0.Slot]struct{}),
		endedSlotOrder:   list.New(),
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
	if _, ended := c.endedSlots[slot]; ended {
		return
	}
	if len(c.pending[slot]) >= MaxPendingPerSlot {
		return
	}
	if _, exists := c.pending[slot]; !exists {
		if len(c.pending) >= MaxPendingSlots {
			// Evict the oldest slot (front of pendingOrder) to bound memory
			// under an attacker creating many distinct slot numbers.
			front := c.pendingOrder.Front()
			if front != nil {
				oldest := front.Value.(phase0.Slot)
				c.pendingOrder.Remove(front)
				delete(c.pendingElem, oldest)
				delete(c.pending, oldest)
			}
		}
		c.pendingElem[slot] = c.pendingOrder.PushBack(slot)
	}
	c.pending[slot] = append(c.pending[slot], env)
}

// DrainPending removes and returns all buffered envelopes for `slot`. Called
// by RunProposerSlot after StartNewInstance so messages that arrived before
// the local instance started are replayed into the now-active instance.
func (c *Controller) DrainPending(slot phase0.Slot) []PendingEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pending[slot]
	c.removePendingLocked(slot)
	return p
}

// ForgetPending evicts buffered envelopes for `slot` without dispatch. Used
// when the slot was never started and the buffer should be cleaned up to
// bound memory.
func (c *Controller) ForgetPending(slot phase0.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removePendingLocked(slot)
}

// removePendingLocked removes a slot from all three pending data structures.
// Caller must hold c.mu.
func (c *Controller) removePendingLocked(slot phase0.Slot) {
	if elem, ok := c.pendingElem[slot]; ok {
		c.pendingOrder.Remove(elem)
		delete(c.pendingElem, slot)
	}
	delete(c.pending, slot)
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

// EndInstance seals the running instance (Finalize sets the ended flag),
// then removes it from the controller's tracking map. Idempotent.
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
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	if !ok {
		return
	}
	r.instanceMu.Lock()
	r.instance.Finalize()
	r.instanceMu.Unlock()
	delete(c.instances, slot)
	c.removePendingLocked(slot)
	c.markSlotEndedLocked(slot)
}

// markSlotEndedLocked records `slot` in the recently-ended ring so
// BufferEnvelope refuses post-teardown buffering for it. FIFO eviction at
// MaxEndedSlots; entries that fall out of the ring are unfenced (and will
// then sit in pending until LRU eviction at MaxPendingSlots — same fate as
// before this fence existed). Caller must hold c.mu.
func (c *Controller) markSlotEndedLocked(slot phase0.Slot) {
	if _, exists := c.endedSlots[slot]; exists {
		return // idempotent: already in ring
	}
	if c.endedSlotOrder.Len() >= MaxEndedSlots {
		front := c.endedSlotOrder.Front()
		if front != nil {
			oldest := front.Value.(phase0.Slot)
			c.endedSlotOrder.Remove(front)
			delete(c.endedSlots, oldest)
		}
	}
	c.endedSlots[slot] = struct{}{}
	c.endedSlotOrder.PushBack(slot)
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
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return nil, ErrNoActiveInstance
	}
	return r.instance.BuildPhase1Bundle(layer, obftcore.Value(value))
}

// ObservePhase1Bundle records a peer's (or own) Phase-1 bundle.
// `observedOffset` is the bundle's first-observation time relative to slot
// start. Bundles past T_accept_max are rejected with ErrLatePhase1Bundle.
func (c *Controller) ObservePhase1Bundle(b *obftcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("obft adapter: nil phase-1 bundle")
	}
	r, err := c.lookup(phase0.Slot(b.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return ErrNoActiveInstance
	}
	return r.instance.ObservePhase1Bundle(b, observedOffset)
}

// ApplyHostValidity records the host application's valid/not-valid verdict
// for `value` at `layer`.
func (c *Controller) ApplyHostValidity(slot phase0.Slot, layer int, value []byte, valid bool) error {
	r, err := c.lookup(slot)
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return ErrNoActiveInstance
	}
	return r.instance.ApplyHostValidity(layer, obftcore.Value(value), valid)
}

// ProcessCommit routes a peer's Commit to the right instance.
func (c *Controller) ProcessCommit(cm *obftcore.Commit) error {
	if cm == nil {
		return errors.New("obft adapter: nil commit")
	}
	r, err := c.lookup(phase0.Slot(cm.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	// Re-check under instanceMu: a goroutine that captured `r` before
	// EndInstance ran could otherwise mutate state on a finalized instance,
	// silently losing late evidence (e.g., a cryptoFake Rule 5 candidate
	// observed AFTER Finalize set the ended flag).
	if r.instance.Ended() {
		return ErrNoActiveInstance
	}
	return r.instance.ObserveCommit(cm)
}

// ProcessCertificate routes a peer's Certificate to the right instance.
func (c *Controller) ProcessCertificate(cert *obftcore.Certificate) error {
	if cert == nil {
		return errors.New("obft adapter: nil certificate")
	}
	r, err := c.lookup(phase0.Slot(cert.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return ErrNoActiveInstance
	}
	return r.instance.ObserveCertificate(cert)
}

// BuildOwnCommit builds the local operator's KindCommit at T_commit. Single
// emission per slot per spec §Phase 2.
func (c *Controller) BuildOwnCommit(slot phase0.Slot) (*obftcore.Commit, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return nil, ErrNoActiveInstance
	}
	return r.instance.BuildOwnCommit()
}

// Resolve runs the Phase-3 reconstruction walk and returns the decided
// Output (value + full reconstructed signature) or an error
// (typically obftcore.ErrNoQuorum if the slot was missed).
func (c *Controller) Resolve(slot phase0.Slot) (*obftcore.Output, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	if r.instance.Ended() {
		return nil, ErrNoActiveInstance
	}
	return r.instance.Resolve()
}

// BuildCertificate produces a final-certificate gossip message for an
// already-resolved Output.
func (c *Controller) BuildCertificate(slot phase0.Slot, out *obftcore.Output) (*obftcore.Certificate, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.BuildCertificate(out)
}

// RetainedCertificate returns a peer-broadcast certificate (if any) — usable
// as an alternative submission path when local Resolve fails.
func (c *Controller) RetainedCertificate(slot phase0.Slot) (*obftcore.Certificate, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.RetainedCertificate(), nil
}

// Evidence returns the slashing-evidence accumulated on this instance.
func (c *Controller) Evidence(slot phase0.Slot) ([]obftcore.Evidence, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.Evidence(), nil
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

func computeLeaderLayers(cfg *obftcore.Config, operatorID spectypes.OperatorID) []int {
	var layers []int
	for i, layer := range cfg.Layers {
		if layer.Leader == obftcore.OperatorID(operatorID) {
			layers = append(layers, i)
		}
	}
	return layers
}
