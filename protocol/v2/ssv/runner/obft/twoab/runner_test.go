package twoab

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	wire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// End-to-end smoke: n=4 operators run one healthy proposer slot through
// RunProposerSlot with stub crypto and a synchronous in-process broadcast bus.
// Verifies all operators converge on the same Output. The real-BLS e2e is L6;
// this exercises the L1–L4 stack wiring.

type smokeNode struct {
	op          spectypes.OperatorID
	ctrl        *Controller
	sched       *Scheduler
	broadcastFn func(data []byte) // set before goroutines launch (happens-before)

	mu        sync.Mutex
	submitted []*twoabcore.Output
}

func (n *smokeNode) submittedOutput() *twoabcore.Output {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.submitted) == 0 {
		return nil
	}
	return n.submitted[0]
}

// smokeBus routes a broadcast to every peer through the real DispatchEnvelope
// router (dispatch.go) — exercising the production inbound path end-to-end
// rather than an inline switch.
type smokeBus struct {
	ctx       context.Context
	slotStart time.Time
	nodes     map[spectypes.OperatorID]*smokeNode
}

func (b *smokeBus) broadcast(from spectypes.OperatorID, data []byte) {
	env, err := wire.Unwrap(data)
	if err != nil {
		return
	}
	for op, p := range b.nodes {
		if op == from {
			continue
		}
		_ = DispatchEnvelope(b.ctx, p.sched, env, time.Since(b.slotStart))
	}
}

func buildSmokeCluster(t *testing.T, n int, overrides *ConfigOverrides) []*smokeNode {
	t.Helper()
	committee := make([]spectypes.OperatorID, n)
	for i := range committee {
		committee[i] = spectypes.OperatorID(i + 1)
	}
	ref, err := ConfigForCluster(0, committee, [32]byte{}, overrides)
	require.NoError(t, err)

	pubShares := make(map[twoabcore.OperatorID][]byte, n)
	for _, o := range committee {
		pubShares[twoabcore.OperatorID(o)] = []byte{byte(o)}
	}

	nodes := make([]*smokeNode, 0, n)
	for _, op := range committee {
		node := &smokeNode{op: op}
		ctrl, err := NewController(ControllerOptions{
			OperatorID:    op,
			Committee:     committee,
			ClusterID:     [32]byte{},
			ClusterPubKey: []byte{0xCC, 0xDD},
			PubKeyShares:  pubShares,
			Signer:        obft.NewStubSigner(ref.QV(), []byte{byte(op)}),
			TagSigner:     obft.NewStubSigner(ref.QV(), []byte{byte(op)}),
			IBE:           obft.NewStubIBE(ref.QEnc()),
			Overrides:     overrides,
		})
		require.NoError(t, err)
		node.ctrl = ctrl

		hooks := &LifecycleHooks{
			FetchCandidate: func(_ context.Context, _ phase0.Slot, layer int) ([]byte, error) {
				return []byte(fmt.Sprintf("L%d-V", layer)), nil
			},
			HostValidate: func(context.Context, phase0.Slot, int, []byte) (bool, error) { return true, nil },
			Broadcast: func(_ context.Context, _ phase0.Slot, data []byte) error {
				if node.broadcastFn != nil {
					node.broadcastFn(data)
				}
				return nil
			},
			SubmitOutput: func(_ context.Context, _ phase0.Slot, out *twoabcore.Output) error {
				node.mu.Lock()
				node.submitted = append(node.submitted, out)
				node.mu.Unlock()
				return nil
			},
		}
		sched, err := NewScheduler(ctrl, hooks)
		require.NoError(t, err)
		node.sched = sched
		nodes = append(nodes, node)
	}
	return nodes
}

func TestRunProposerSlot_Healthy_n4(t *testing.T) {
	// Compressed timing: TPhase2a backstop at 120ms (early-fire via L0ReadyCh
	// dominates the healthy path), leaders broadcast early. The synchronous bus
	// gives instant propagation, so convergence is near-immediate; the 3s ctx
	// is just an upper bound.
	overrides := &ConfigOverrides{
		BTT:              20 * time.Millisecond,
		tPhase2aOverride: 120 * time.Millisecond,
		FetchAt:          []time.Duration{40 * time.Millisecond, 0},
		BroadcastBudget:  []time.Duration{40 * time.Millisecond, 60 * time.Millisecond},
	}
	nodes := buildSmokeCluster(t, 4, overrides)

	const slot = phase0.Slot(0)
	slotStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	bus := &smokeBus{ctx: ctx, slotStart: slotStart, nodes: make(map[spectypes.OperatorID]*smokeNode, len(nodes))}
	for _, n := range nodes {
		n := n
		n.broadcastFn = func(data []byte) { bus.broadcast(n.op, data) }
		bus.nodes[n.op] = n
	}

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()

	var ref *twoabcore.Output
	for _, n := range nodes {
		out := n.submittedOutput()
		require.NotNilf(t, out, "op %d submitted no output", n.op)
		if ref == nil {
			ref = out
			continue
		}
		require.Equalf(t, ref.Value, out.Value, "op %d decided a different Value", n.op)
		require.Equalf(t, ref.Signature, out.Signature, "op %d reconstructed a different Signature", n.op)
	}
	require.Equal(t, []byte("L0-V"), []byte(ref.Value), "cluster decides the L_0 leader's candidate")
}
