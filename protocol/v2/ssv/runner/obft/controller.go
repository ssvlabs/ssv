package obft

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
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

	mu        sync.Mutex
	instances map[phase0.Slot]*RunningInstance
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
		operatorID:      opts.OperatorID,
		committee:       committee,
		clusterID:       opts.ClusterID,
		clusterPubKey:   opts.ClusterPubKey,
		pubKeyShares:    opts.PubKeyShares,
		ibePubKeyShares: opts.IBEPubKeyShares,
		signer:          opts.Signer,
		tagSigner:       opts.TagSigner,
		ibe:             opts.IBE,
		overrides:       opts.Overrides,
		instances:       make(map[phase0.Slot]*RunningInstance),
	}, nil
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

// EndInstance removes the running instance for `slot`. Idempotent.
func (c *Controller) EndInstance(slot phase0.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.instances, slot)
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
		return nil, fmt.Errorf("obft adapter: no active instance for slot %d", slot)
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
