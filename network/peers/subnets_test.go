package peers

import (
	"github.com/bloxapp/ssv/network/records"
	nettesting "github.com/bloxapp/ssv/network/testing"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubnetsIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(4)
	require.NoError(t, err)

	var pids []peer.ID
	for _, nk := range nks {
		pid, err := peer.IDFromPrivateKey(crypto.PrivKey((*crypto.Secp256k1PrivateKey)(nk.NetKey)))
		require.NoError(t, err)
		pids = append(pids, pid)
	}

	sAll, err := records.Subnets{}.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	sNone, err := records.Subnets{}.FromString("0x00000000000000000000000000000000")
	require.NoError(t, err)
	sPartial, err := records.Subnets{}.FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	subnetsIdx := newSubnetsIndex(128)

	subnetsIdx.SaveSubnets(pids[0], sAll)
	subnetsIdx.SaveSubnets(pids[1], sNone)
	subnetsIdx.SaveSubnets(pids[2], sPartial)

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 2)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 1)
}
