package tbft

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
)

// Controller is the SSV-side state-machine wrapper around tbft.Instance.
// It owns the per-cluster identity material (operator share, cluster pubkey,
// etc.) and a map of currently-active TBFT instances keyed by slot.
//
// The Controller is per (operator, cluster, role) — analogous to one QBFT
// Controller in SSV's existing structure. It mirrors `protocol/v2/qbft/controller.Controller`
// in scope but with a TBFT-shaped API: no propose/prepare/commit phases,
// no round timer, no separate post-consensus.
//
// Lifecycle (driven by the runner):
//
//	StartNewInstance(slot) → RunningInstance
//	  ObserveCandidate(slot, layer, value)  // for layers we / others lead
//	  ProcessOnion(o) / ProcessNonReceipt(nr)  // as peers' messages arrive
//	  BuildOwnOnion(slot) → Onion
//	  BuildOwnNonReceipts(slot) → []*NonReceiptAttestation
//	Resolve(slot) → *Output, error
//	EndInstance(slot)
//
// Concurrency: all methods are safe to call from multiple goroutines
// (operations are mutexed). Internal Instance access is serialized.
type Controller struct {
	operatorID    spectypes.OperatorID
	committee     []spectypes.OperatorID
	clusterID     [32]byte
	clusterPubKey []byte
	pubKeyShares  map[tbftcore.OperatorID][]byte

	signer    tbftcore.Signer
	tagSigner tbftcore.Signer // may be nil → falls back to signer in Instance ctor
	ibe       tbftcore.ThresholdIBE
	overrides *ConfigOverrides

	mu        sync.Mutex
	instances map[phase0.Slot]*RunningInstance
}

// RunningInstance bundles a started TBFT instance with the metadata the
// runner needs to drive Phase 1 / 2 / 3 (specifically, knowledge of which
// layers the local operator is the leader for so the runner can schedule
// candidate fetches).
type RunningInstance struct {
	Slot   phase0.Slot
	Config *tbftcore.Config

	// LeaderAtLayers lists the layer indices where the local operator is
	// the designated leader for this slot. The runner uses these to
	// schedule candidate fetches (Phase 1) at the layer's FetchAt offset.
	// Empty if the local operator is not a leader for this slot.
	LeaderAtLayers []int

	// instanceMu serializes access to `instance`. The Controller's own
	// mutex (`c.mu`) only protects the `instances` map; per-Instance
	// access needs its own lock so concurrent ProcessOnion /
	// ProcessNonReceipt / ProcessCandidate / Resolve calls (which can
	// originate from different goroutines — message dispatch + the
	// runner's own scheduling) don't race on the Instance's internal
	// state.
	instanceMu sync.Mutex
	instance   *tbftcore.Instance
}

// ControllerOptions parameterizes Controller construction.
type ControllerOptions struct {
	// OperatorID is the local operator's ID within the cluster.
	OperatorID spectypes.OperatorID

	// Committee is the full list of operators in the cluster (sorted or
	// unsorted; ConfigForCluster sorts internally).
	Committee []spectypes.OperatorID

	// ClusterID uniquely identifies the cluster (used in IBE tags).
	ClusterID [32]byte

	// ClusterPubKey is the cluster's threshold BLS public key (the
	// validator's pubkey under Option A, the IBE trust anchor).
	ClusterPubKey []byte

	// PubKeyShares maps each operator's ID to their pubkey share, used
	// to verify per-partial signatures during Resolve.
	PubKeyShares map[tbftcore.OperatorID][]byte

	// Signer is used for value-signing (Phase-2 onion contents) and for
	// per-partial verification during Resolve. For SSV this is typically
	// `blsbackend.BLSSigner` (herumi-backed, Eth2 DST).
	Signer tbftcore.Signer

	// TagSigner is used for IBE-tag signing (NonReceiptAttestations) and
	// for aggregating non-receipts into IBE decryption keys. If nil,
	// `Signer` is used for both purposes — sufficient when the IBE
	// implementation accepts the value-signer's aggregate format (e.g.
	// `SignerGatedIBE` or `StubIBE`).
	//
	// For real cryptographic IBE under the DST-trick approach (see
	// docs/IBE-INTEGRATION.md), set this to `blsbackend.KyberSigner`
	// alongside an IBE of `blsbackend.TLockIBE`.
	TagSigner tbftcore.Signer

	// IBE — concrete primitive backend for layer-encryption.
	IBE tbftcore.ThresholdIBE

	// Overrides — optional protocol parameter overrides; nil uses defaults.
	Overrides *ConfigOverrides
}

// NewController constructs a Controller from the given options.
func NewController(opts ControllerOptions) (*Controller, error) {
	if opts.Signer == nil {
		return nil, errors.New("tbft adapter: nil Signer")
	}
	if opts.IBE == nil {
		return nil, errors.New("tbft adapter: nil IBE")
	}
	if opts.PubKeyShares == nil {
		return nil, errors.New("tbft adapter: nil PubKeyShares")
	}
	if len(opts.Committee) == 0 {
		return nil, errors.New("tbft adapter: empty committee")
	}

	// Defensive copies for fields the caller might mutate later.
	committee := make([]spectypes.OperatorID, len(opts.Committee))
	copy(committee, opts.Committee)

	return &Controller{
		operatorID:    opts.OperatorID,
		committee:     committee,
		clusterID:     opts.ClusterID,
		clusterPubKey: opts.ClusterPubKey,
		pubKeyShares:  opts.PubKeyShares,
		signer:        opts.Signer,
		tagSigner:     opts.TagSigner, // may be nil; Instance ctor handles fallback
		ibe:           opts.IBE,
		overrides:     opts.Overrides,
		instances:     make(map[phase0.Slot]*RunningInstance),
	}, nil
}

// StartNewInstance initializes a TBFT instance for the given slot and
// records it in the active-instances map. Returns a RunningInstance with
// the Config and the layer indices where the local operator is the leader.
//
// Errors if an instance already exists for `slot` (duplicate-start), or if
// the cluster config cannot be built for the slot.
func (c *Controller) StartNewInstance(slot phase0.Slot) (*RunningInstance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.instances[slot]; exists {
		return nil, fmt.Errorf("tbft adapter: instance already running for slot %d", slot)
	}

	cfg, err := ConfigForCluster(slot, c.committee, c.clusterID, c.overrides)
	if err != nil {
		return nil, fmt.Errorf("tbft adapter: build config: %w", err)
	}

	inst, err := tbftcore.NewInstanceWithTagSigner(
		cfg, c.signer, c.tagSigner, c.ibe, c.clusterPubKey, c.pubKeyShares,
	)
	if err != nil {
		return nil, fmt.Errorf("tbft adapter: new instance: %w", err)
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

// OperatorID returns the local operator's ID. Callers that wrap this
// Controller (e.g. the Scheduler) need it to fill in the OperatorID field
// when constructing outbound messages on behalf of the local operator.
func (c *Controller) OperatorID() spectypes.OperatorID {
	return c.operatorID
}

// EndInstance removes the running instance for `slot`. Idempotent — calling
// for a slot with no active instance is a no-op.
func (c *Controller) EndInstance(slot phase0.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.instances, slot)
}

// ActiveSlots returns the slots with currently-running instances, sorted
// ascending. Useful for debugging / metrics.
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

// ObserveCandidate records a Phase-1 candidate value at the given layer
// for the slot's instance. The runner calls this both for its own
// fetched candidates (when it's a layer leader) and for candidates
// received from peers via gossip.
func (c *Controller) ObserveCandidate(slot phase0.Slot, layer int, value []byte) error {
	r, err := c.lookup(slot)
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.ObserveCandidate(layer, tbftcore.Value(value))
}

// ProcessOnion routes a peer's onion to the right instance. The slot is
// inferred from the onion's Height field.
func (c *Controller) ProcessOnion(o *tbftcore.Onion) error {
	if o == nil {
		return errors.New("tbft adapter: nil onion")
	}
	r, err := c.lookup(phase0.Slot(o.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.ObserveOnion(o)
}

// ProcessNonReceipt routes a peer's non-receipt attestation to the right
// instance. The slot is inferred from the message's Height.
func (c *Controller) ProcessNonReceipt(nr *tbftcore.NonReceiptAttestation) error {
	if nr == nil {
		return errors.New("tbft adapter: nil non-receipt")
	}
	r, err := c.lookup(phase0.Slot(nr.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.ObserveNonReceipt(nr)
}

// ProcessCandidate routes a peer's Phase-1 candidate broadcast to the right
// instance. The slot is inferred from the message's Height.
//
// The runner typically calls this when receiving a wire-envelope message
// whose Kind is KindCandidate. It is also legitimate (and expected) for the
// local operator to call this on its OWN candidate after fetching, so the
// operator's own onion at Phase 2 includes the candidate's value.
func (c *Controller) ProcessCandidate(cb *tbftcore.CandidateBroadcast) error {
	if cb == nil {
		return errors.New("tbft adapter: nil candidate broadcast")
	}
	r, err := c.lookup(phase0.Slot(cb.Height))
	if err != nil {
		return err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.ObserveCandidate(cb.Layer, cb.Value)
}

// BuildOwnOnion produces the local operator's onion for the slot's
// instance. Signing uses the share-bound Signer the Controller was
// constructed with.
func (c *Controller) BuildOwnOnion(slot phase0.Slot) (*tbftcore.Onion, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.BuildOwnOnion(tbftcore.OperatorID(c.operatorID))
}

// BuildOwnNonReceipts produces the local operator's non-receipt
// attestations for layers without a candidate. Signing uses the share-
// bound TagSigner (or Signer, if no separate TagSigner is configured)
// the Controller was constructed with.
func (c *Controller) BuildOwnNonReceipts(slot phase0.Slot) ([]*tbftcore.NonReceiptAttestation, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.BuildOwnNonReceipts(tbftcore.OperatorID(c.operatorID))
}

// Resolve runs the Phase-3 decryption walk for the slot's instance and
// returns the decided Output (value + full reconstructed signature) or
// an error (typically tbftcore.ErrNoQuorum if the slot was missed).
//
// Resolve does NOT remove the instance — call EndInstance when the runner
// is done with it (e.g. after submission to the beacon node).
func (c *Controller) Resolve(slot phase0.Slot) (*tbftcore.Output, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.Resolve()
}

// InconsistencyFaults returns the inconsistency-fault list (operators who
// signed both a positive contribution and a non-receipt for the same layer)
// for the slot's instance. Empty if no faults were observed during Resolve.
func (c *Controller) InconsistencyFaults(slot phase0.Slot) ([]tbftcore.InconsistencyFault, error) {
	r, err := c.lookup(slot)
	if err != nil {
		return nil, err
	}
	r.instanceMu.Lock()
	defer r.instanceMu.Unlock()
	return r.instance.InconsistencyFaults(), nil
}

// lookup is the common "find instance for this slot or return ErrNoSuchInstance"
// helper used by all per-instance methods.
func (c *Controller) lookup(slot phase0.Slot) (*RunningInstance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.instances[slot]
	if !ok {
		return nil, fmt.Errorf("tbft adapter: no active instance for slot %d", slot)
	}
	return r, nil
}

// computeLeaderLayers returns the layer indices in `cfg.Layers` where the
// given operator is the leader. Empty if the operator is not a leader at
// any layer of this slot.
func computeLeaderLayers(cfg *tbftcore.Config, operatorID spectypes.OperatorID) []int {
	var layers []int
	for i, layer := range cfg.Layers {
		if layer.Leader == tbftcore.OperatorID(operatorID) {
			layers = append(layers, i)
		}
	}
	return layers
}
