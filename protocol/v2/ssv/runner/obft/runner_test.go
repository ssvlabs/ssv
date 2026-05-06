package obft

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

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/utils/threshold"
)

// End-to-end runner test: drives n=4 operators through one healthy proposer-
// duty slot using RunProposerSlot, with sub-second timing so tests run fast.
// Verifies all operators converge on the same Output and that SubmitOutput
// fires.

type runnerNode struct {
	op    spectypes.OperatorID
	ctrl  *Controller
	sched *Scheduler
	hooks *runnerHooks
}

func (n *runnerNode) submittedOutput() *obftcore.Output {
	n.hooks.mu.Lock()
	defer n.hooks.mu.Unlock()
	if len(n.hooks.submitted) == 0 {
		return nil
	}
	return n.hooks.submitted[0]
}

func TestRunProposerSlot_Healthy_n4_K4(t *testing.T) {
	// Compressed timing for fast tests. The relative phase ordering matches
	// production but with smaller absolute durations.
	overrides := &ConfigOverrides{
		K:       4,
		TCommit: 200 * time.Millisecond,
		Delta2:  60 * time.Millisecond, // 2*(D+δ)
		Delta3:  60 * time.Millisecond,
		D:       20 * time.Millisecond,
		Delta:   10 * time.Millisecond,
		// T_broadcast_max = TCommit - 2*(D+δ) = 200 - 60 = 140ms; all
		// FetchAt entries must be ≤ 140 and non-increasing in k.
		FetchAt: []time.Duration{
			130 * time.Millisecond,
			110 * time.Millisecond,
			90 * time.Millisecond,
			70 * time.Millisecond,
		},
	}

	nodes := buildCluster(t, 4, overrides)

	const slot = phase0.Slot(7)
	slotStart := time.Now()

	bus := newBroadcastBus(nodes, slotStart)
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
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	var ref *obftcore.Output
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
	require.Equal(t, 0, ref.Layer, "healthy case should decide at L_0")
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

	pubShares := make(map[obftcore.OperatorID][]byte, n)
	for id, sk := range shares {
		pubShares[obftcore.OperatorID(id)] = sk.GetPublicKey().Serialize()
	}

	committee := make([]spectypes.OperatorID, n)
	for i := 0; i < n; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}

	verifier := blsbackend.New(nil)
	ibe := blsbackend.NewSignerGatedIBE(verifier, masterPub)

	nodes := make([]*runnerNode, 0, n)
	for _, op := range committee {
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
		hooks.fetchFn = func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
			if hooks.fetchErr != nil {
				return nil, hooks.fetchErr
			}
			return []byte(fmt.Sprintf("slot-%d-layer-%d-block", slot, layer)), nil
		}
		hooks.hostValidateFn = func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
			return true, nil
		}

		s, err := NewScheduler(ctrl, hooks.LifecycleHooks())
		require.NoError(t, err)

		nodes = append(nodes, &runnerNode{op: op, ctrl: ctrl, sched: s, hooks: hooks})
	}
	return nodes
}

// ---- broadcast bus -------------------------------------------------------

type broadcastBus struct {
	nodes     []*runnerNode
	slotStart time.Time
	wg        sync.WaitGroup
	once      sync.Once
}

func newBroadcastBus(nodes []*runnerNode, slotStart time.Time) *broadcastBus {
	return &broadcastBus{nodes: nodes, slotStart: slotStart}
}

func (b *broadcastBus) broadcast(from spectypes.OperatorID, data []byte) {
	observedOffset := time.Since(b.slotStart)
	for _, n := range b.nodes {
		if n.op == from {
			continue
		}
		n := n
		dataCopy := append([]byte{}, data...)
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			_ = DispatchBytes(context.Background(), n.sched, dataCopy, observedOffset)
		}()
	}
}

func (b *broadcastBus) stop() {
	b.once.Do(func() {
		b.wg.Wait()
	})
}

// ---- runnerHooks --------------------------------------------------------

type runnerHooks struct {
	op spectypes.OperatorID

	mu sync.Mutex

	fetchFn        func(context.Context, phase0.Slot, int) ([]byte, error)
	fetchErr       error
	hostValidateFn func(context.Context, phase0.Slot, int, []byte) (bool, error)
	broadcastFn    func(context.Context, phase0.Slot, []byte) error

	submitted []*obftcore.Output
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
		HostValidate: func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error) {
			h.mu.Lock()
			fn := h.hostValidateFn
			h.mu.Unlock()
			if fn == nil {
				return true, nil
			}
			return fn(ctx, slot, layer, value)
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
		SubmitOutput: func(ctx context.Context, slot phase0.Slot, out *obftcore.Output) error {
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
