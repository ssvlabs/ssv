package p2pv1

import (
	"context"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // tears down New()'s internal goroutines; Close() can't be used here (it nil-panics before Setup)

	n, err := New(zap.NewNop(), &Config{Ctx: ctx})
	require.NoError(t, err)

	// New() leaves the network in stateClosed; only Start() flips it to stateReady.
	require.False(t, n.isReady(), "a freshly constructed (un-Started) network must not be ready")

	// Both subscribe paths used by operator.Node.Start reject an un-Started network.
	require.ErrorIs(t, n.SubscribeRandoms(1), p2pprotocol.ErrNetworkIsNotReady)
	require.ErrorIs(t, n.SubscribeAll(), p2pprotocol.ErrNetworkIsNotReady)
}
