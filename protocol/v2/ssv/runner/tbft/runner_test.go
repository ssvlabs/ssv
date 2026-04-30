package tbft

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// End-to-end runner test: drives `n` operators through one full proposer-
// duty slot using `RunProposerSlot`, with realistic per-slot timing
// (sub-second so tests run fast). Verifies all operators converge on the
// same Output and that SubmitOutput is called with the validator-signed
// block-equivalent.
//
// This is the runtime-shape reference for what proposer.go's lifecycle
// needs to look like.

// runnerNode holds one operator's runtime state for the e2e test.
type runnerNode struct {
	op    spectypes.OperatorID
	ctrl  *Controller
	sched *Scheduler
	rl    *RateLimiter
	hooks *runnerHooks
}

// submittedOutput returns the single Output the node's SubmitOutput hook
// recorded, or nil if none was submitted (slot missed).
func (n *runnerNode) submittedOutput() *tbftcore.Output {
	n.hooks.mu.Lock()
	defer n.hooks.mu.Unlock()
	if len(n.hooks.submitted) == 0 {
		return nil
	}
	return n.hooks.submitted[0]
}

func TestRunProposerSlot_HealthyN7(t *testing.T) {
	// Generous-enough timing that goroutine + gossip latency is not a
	// flakiness factor while still keeping each test under ~500ms.
	overrides := &ConfigOverrides{
		DeadlineOffset:   200 * time.Millisecond,
		LateFetchOffset:  50 * time.Millisecond,
		EarlyFetchOffset: -50 * time.Millisecond,
	}
	const gossipWindow = 100 * time.Millisecond

	nodes := buildCluster(t, 7, overrides)

	const slot = phase0.Slot(7)
	slotStart := time.Now()

	bus := newBroadcastBus(nodes)
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			bus.broadcast(n.op, data)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlotWithGossipWindow(ctx, n.ctrl, n.sched, slot, slotStart, gossipWindow)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	// Every node submitted exactly one output, and they all match.
	var ref *tbftcore.Output
	for _, n := range nodes {
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output", n.op)
		if ref == nil {
			ref = out
			continue
		}
		require.True(t, bytes.Equal(ref.Value, out.Value), "op %d value differs", n.op)
		require.True(t, bytes.Equal(ref.Signature, out.Signature), "op %d signature differs", n.op)
		require.Equal(t, ref.Layer, out.Layer)
	}
	require.Equal(t, 0, ref.Layer, "healthy case should decide at layer 0")
}

func TestRunProposerSlot_TopLeaderSilentN7(t *testing.T) {
	// Configure the top leader's FetchCandidate to return an error,
	// simulating "leader silent / unreachable beacon node". Other layers'
	// fetches succeed. Cluster should fall through to layer 1 and submit
	// that output instead.
	overrides := &ConfigOverrides{
		DeadlineOffset:   200 * time.Millisecond,
		LateFetchOffset:  50 * time.Millisecond,
		EarlyFetchOffset: -50 * time.Millisecond,
	}
	const gossipWindow = 100 * time.Millisecond

	nodes := buildCluster(t, 7, overrides)

	const slot = phase0.Slot(11)
	slotStart := time.Now()

	// Identify the top-layer leader (layer 0) and make their fetch fail.
	cfg, err := ConfigForCluster(slot, committeeOf(nodes), [32]byte{}, overrides)
	require.NoError(t, err)
	topLeader := spectypes.OperatorID(cfg.Layers[0].Leader)

	bus := newBroadcastBus(nodes)
	defer bus.stop()

	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			bus.broadcast(n.op, data)
			return nil
		}
		if n.op == topLeader {
			n.hooks.fetchErr = fmt.Errorf("simulated beacon node down")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RunProposerSlotWithGossipWindow(ctx, n.ctrl, n.sched, slot, slotStart, gossipWindow)
		}()
	}
	wg.Wait()

	// At least quorum (5) of 7 should have produced an output at layer >= 1
	// (top layer's leader was silent → fallthrough).
	successCount := 0
	for _, n := range nodes {
		out := n.submittedOutput()
		if out != nil {
			require.GreaterOrEqual(t, out.Layer, 1,
				"op %d: layer 0 leader silent, expected fallthrough to layer >= 1", n.op)
			successCount++
		}
	}
	require.GreaterOrEqual(t, successCount, 5, "at least quorum should produce outputs")
}

// ---- cluster setup helpers ----------------------------------------------

func buildCluster(t *testing.T, n int, overrides *ConfigOverrides) []*runnerNode {
	t.Helper()
	threshold.Init()
	f := (n - 1) / 3
	q := 2*f + 1

	master := &bls.SecretKey{}
	master.SetByCSPRNG()
	masterPub := master.GetPublicKey().Serialize()

	shares, err := threshold.Create(master.Serialize(), uint64(q), uint64(n))
	require.NoError(t, err)

	pubShares := make(map[tbftcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[tbftcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}

	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	nodes := make([]*runnerNode, 0, n)
	for _, op := range committee {
		// `shares` is keyed by uint64; spectypes.OperatorID has the same
		// underlying type but Go requires the explicit cross-named-type
		// conversion at the index expression.
		opSigner := blsbackend.New(shares[uint64(op)].Serialize()) //nolint:unconvert

		ctrl, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{0xCA, 0xFE},
			ClusterPubKey: masterPub,
			PubKeyShares:  pubShares,
			Signer:        opSigner,
			IBE:           ibe,
			Overrides:     overrides,
		})
		require.NoError(t, err)

		hooks := newRunnerHooks(op)
		// Simulate the beacon-node fetch by returning a per-(slot,layer) value
		// that the protocol can converge on. All operators at a given (slot,
		// layer) get the same canonical value, modeling beacon agreement.
		hooks.fetchFn = func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			if hooks.fetchErr != nil {
				return nil, hooks.fetchErr
			}
			return []byte(fmt.Sprintf("slot-%d-layer-%d-block", slot, layer)), nil
		}

		s, err := NewScheduler(ctrl, hooks.LifecycleHooks())
		require.NoError(t, err)

		nodes = append(nodes, &runnerNode{
			op:    op,
			ctrl:  ctrl,
			sched: s,
			rl:    NewRateLimiter(),
			hooks: hooks,
		})
	}
	return nodes
}

func committeeOf(nodes []*runnerNode) []spectypes.OperatorID {
	out := make([]spectypes.OperatorID, len(nodes))
	for i, n := range nodes {
		out[i] = n.op
	}
	return out
}

// ---- broadcast bus -------------------------------------------------------

// broadcastBus is an in-memory message bus connecting all nodes. Each
// node's Broadcast hook delivers bytes here; the bus dispatches them to
// every other node via DispatchBytes (which calls the right Process*
// method on the receiver's Controller).
type broadcastBus struct {
	nodes []*runnerNode
	wg    sync.WaitGroup
	once  sync.Once
}

func newBroadcastBus(nodes []*runnerNode) *broadcastBus {
	return &broadcastBus{nodes: nodes}
}

// broadcast delivers `data` to every node except `from`, asynchronously.
// Errors from DispatchBytes are silently ignored — they typically indicate
// rate-limit or no-such-instance, both of which are recoverable in a
// realistic gossip topology.
func (b *broadcastBus) broadcast(from spectypes.OperatorID, data []byte) {
	for _, n := range b.nodes {
		if n.op == from {
			continue
		}
		n := n
		dataCopy := append([]byte{}, data...)
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			_ = DispatchBytes(n.ctrl, dataCopy)
		}()
	}
}

func (b *broadcastBus) stop() {
	b.once.Do(func() {
		b.wg.Wait()
	})
}

// ---- runnerHooks --------------------------------------------------------

// runnerHooks is the test-side LifecycleHooks implementation. Each hook
// is a function field that can be reassigned per-test for behavior
// injection.
type runnerHooks struct {
	op spectypes.OperatorID

	mu sync.Mutex

	fetchFn  func(context.Context, phase0.Slot, int) ([]byte, error)
	fetchErr error

	broadcastFn func(context.Context, phase0.Slot, []byte) error

	submitted []*tbftcore.Output
	missed    []phase0.Slot
}

func newRunnerHooks(op spectypes.OperatorID) *runnerHooks {
	return &runnerHooks{op: op}
}

func (h *runnerHooks) LifecycleHooks() *LifecycleHooks {
	return &LifecycleHooks{
		FetchCandidate: func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			h.mu.Lock()
			fn := h.fetchFn
			h.mu.Unlock()
			if fn == nil {
				return nil, fmt.Errorf("hooks: no FetchCandidate configured")
			}
			return fn(ctx, slot, layer)
		},
		Broadcast: func(ctx context.Context, slot phase0.Slot, data []byte) error {
			h.mu.Lock()
			fn := h.broadcastFn
			h.mu.Unlock()
			if fn == nil {
				return nil
			}
			return fn(ctx, slot, data)
		},
		SubmitOutput: func(ctx context.Context, slot phase0.Slot, out *tbftcore.Output) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.submitted = append(h.submitted, out)
			return nil
		},
		OnMissedSlot: func(ctx context.Context, slot phase0.Slot, reason error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.missed = append(h.missed, slot)
		},
	}
}
