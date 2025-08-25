package peers

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	nettesting "github.com/ssvlabs/ssv/network/testing"
)

func TestScoresIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(1)
	require.NoError(t, err)

	sk, err := commons.ECDSAPrivToInterface(nks[0].NetKey)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	si := newScoreIndex()

	require.NoError(t, si.Score(pid, &NodeScore{
		Name:  "decided",
		Value: 1.0,
	}, &NodeScore{
		Name:  "relays",
		Value: -2.0,
	}))

	scores, err := si.GetScore(pid, "decided", "relays", "dummy")
	require.NoError(t, err)
	require.Len(t, scores, 2)
}
