package peertrace

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

const attackSimulatorPublicKey = "0x02006c0a9a7e965cb22399987a5a748e90bcc4cb76c461b5d62643c2f2f112055e"

func TestNew_PublicKeyDerivesHighlightedPeer(t *testing.T) {
	observer, err := New(Config{
		Label: "attack-simulator",
		Peers: attackSimulatorPublicKey,
	})
	require.NoError(t, err)
	require.True(t, observer.Enabled())
	require.Equal(t, 1, observer.Count())

	var matched Peer
	for pid := range observer.peers {
		var ok bool
		matched, ok = observer.Match(pid)
		require.True(t, ok)
	}
	require.NotEmpty(t, matched.ID)
	require.Equal(t, "public_key", matched.Source)
	require.Equal(t, attackSimulatorPublicKey, matched.PublicKeyHex)
}

func TestNew_AcceptsMixedPeerList(t *testing.T) {
	pid, err := peer.Decode("12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE")
	require.NoError(t, err)

	observer, err := New(Config{
		Peers: attackSimulatorPublicKey + ", 12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE",
	})
	require.NoError(t, err)
	require.Equal(t, 2, observer.Count())

	matched, ok := observer.Match(pid)
	require.True(t, ok)
	require.Equal(t, "peer_id", matched.Source)
}

func TestNew_EmptyConfigDisablesObserver(t *testing.T) {
	observer, err := New(Config{})
	require.NoError(t, err)
	require.Nil(t, observer)
}
