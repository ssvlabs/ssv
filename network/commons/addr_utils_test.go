package commons

import (
	crand "crypto/rand"
	"fmt"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
)

func Test_BuildMultiAddress(t *testing.T) {
	sk, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	t.Run("IPv4", func(t *testing.T) {
		ma, err := BuildMultiAddress(DefaultIP, "tcp", DefaultTCP, id)
		require.NoError(t, err)
		expected := fmt.Sprintf("/ip4/%s/%s/%d/p2p/%s", DefaultIP, "tcp", DefaultTCP, id.String())
		require.Equal(t, expected, ma.String())
	})

	t.Run("IPv6", func(t *testing.T) {
		ma, err := BuildMultiAddress(net.IPv6zero.String(), "tcp", DefaultTCP, id)
		require.NoError(t, err)
		expected := fmt.Sprintf("/ip6/%s/%s/%d/p2p/%s", net.IPv6zero.String(), "tcp", DefaultTCP, id.String())
		require.Equal(t, expected, ma.String())
	})
}
