package p2pv1

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

// TestSubscribeRequiresStartedNetwork locks in the invariant that a constructed-but-not-Started
// network rejects subscriptions. It is the p2p-level guard behind the cli/operator fix that moved
// p2pNetwork.Setup()/Start() out of the `if DynamicMaxPeers` block: gating the network lifecycle
// on that flag left DynamicMaxPeers=false nodes with an un-Started network whose Subscribe* calls
// (made by operator.Node.Start) failed with ErrNetworkIsNotReady.
func TestSubscribeRequiresStartedNetwork(t *testing.T) {
	n, err := New(zap.NewNop(), &Config{Ctx: context.Background()})
	require.NoError(t, err)
	defer func() { require.NoError(t, n.Close()) }() // Close() is safe on an un-Setup network (idempotent, nil-guarded)

	// New() leaves the network in stateClosed; only Start() flips it to stateReady.
	require.False(t, n.isReady(), "a freshly constructed (un-Started) network must not be ready")

	// Both subscribe paths used by operator.Node.Start reject an un-Started network.
	require.ErrorIs(t, n.SubscribeRandoms(1), p2pprotocol.ErrNetworkIsNotReady)
	require.ErrorIs(t, n.SubscribeAll(), p2pprotocol.ErrNetworkIsNotReady)
}

// TestClose_CreatedOnly_Idempotent verifies Close() is a safe no-op (no nil-panic) on a network
// that was constructed but never Setup/Started, and that it stays a no-op under repeated calls.
// The CLI defers Close() right after constructing the network, so this path must be safe.
func TestClose_CreatedOnly_Idempotent(t *testing.T) {
	n, err := New(zap.NewNop(), &Config{Ctx: context.Background()})
	require.NoError(t, err)

	// Close() cancels the network's context, so it also tears down New()'s internal goroutines.
	require.NotPanics(t, func() {
		require.NoError(t, n.Close())
		require.NoError(t, n.Close()) // double-close is a no-op
	})
}

// TestClose_PartialSetup_NoPanic verifies Close() nil-guards its components: a Setup that started
// (state=initializing) but failed before allocating host/services leaves those fields nil, and
// Close() must not nil-panic on them — exactly the assembly-failure path the CLI defer must cover.
func TestClose_PartialSetup_NoPanic(t *testing.T) {
	n, err := New(zap.NewNop(), &Config{Ctx: context.Background()})
	require.NoError(t, err)

	// Simulate "Setup started, components not yet allocated".
	atomic.StoreInt32(&n.state, stateInitializing)

	// Close() (which cancels the context) also tears down New()'s internal goroutines.
	require.NotPanics(t, func() { require.NoError(t, n.Close()) })
}

// TestClose_RunsTeardownRegardlessOfState guards the closeOnce-vs-state distinction: a network
// whose state already reads stateClosed (e.g. Start() failed and reset it, leaving host/services
// allocated) must still run teardown on the first Close — verified via the context being canceled.
// A state-based idempotency guard would wrongly skip it and leak those components.
func TestClose_RunsTeardownRegardlessOfState(t *testing.T) {
	n, err := New(zap.NewNop(), &Config{Ctx: context.Background()})
	require.NoError(t, err)

	atomic.StoreInt32(&n.state, stateClosed) // simulate a Start-failed network (state reset, components allocated)
	require.NoError(t, n.Close())

	select {
	case <-n.ctx.Done(): // teardown ran: cancel() was called
	default:
		t.Fatal("Close did not run teardown (cancel) on a stateClosed network")
	}
}
