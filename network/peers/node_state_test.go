package peers

import (
	nettesting "github.com/bloxapp/ssv/network/testing"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNodeStates(t *testing.T) {
	nks, err := nettesting.CreateKeys(4)
	require.NoError(t, err)

	var pids []peer.ID
	for _, nk := range nks {
		pid, err := peer.IDFromPrivateKey(crypto.PrivKey((*crypto.Secp256k1PrivateKey)(nk.NetKey)))
		require.NoError(t, err)
		pids = append(pids, pid)
	}

	t.Run("State", func(t *testing.T) {
		ns := newNodeStates(time.Minute)

		require.Equal(t, StateUnknown, ns.State(pids[0]))
		ns.setState(pids[0].String(), StateReady)
		require.Equal(t, StateReady, ns.State(pids[0]))
	})

	t.Run("Prune", func(t *testing.T) {
		ns := newNodeStates(time.Minute)

		ns.setState(pids[0].String(), StateReady)
		require.NoError(t, ns.Prune(pids[1]))
		require.NoError(t, ns.Prune(pids[2]))
		require.NoError(t, ns.Prune(pids[3]))
		require.NoError(t, ns.Prune(pids[0]))

		for _, pid := range pids {
			require.Equal(t, StatePruned, ns.State(pid))
		}
	})

	t.Run("EvictPrune", func(t *testing.T) {
		ns := newNodeStates(time.Minute)

		require.NoError(t, ns.Prune(pids[0]))
		require.NoError(t, ns.Prune(pids[1]))

		require.Equal(t, StatePruned, ns.State(pids[0]))
		require.Equal(t, StatePruned, ns.State(pids[1]))

		ns.EvictPruned(pids[0])

		require.Equal(t, StateReady, ns.State(pids[0]))
		require.Equal(t, StatePruned, ns.State(pids[1]))
	})

	t.Run("GC", func(t *testing.T) {
		ns := newNodeStates(time.Millisecond * 10)

		require.NoError(t, ns.Prune(pids[0]))
		require.NoError(t, ns.Prune(pids[1]))

		require.Equal(t, StatePruned, ns.State(pids[0]))
		require.Equal(t, StatePruned, ns.State(pids[1]))

		<-time.After(time.Millisecond * 10 * 2)

		ns.GC()

		require.Equal(t, StateUnknown, ns.State(pids[0]))
		require.Equal(t, StateUnknown, ns.State(pids[1]))
	})
}
