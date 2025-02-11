package commons

import (
	"crypto/rand"
	"math/big"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestCommitteeSubnet(t *testing.T) {
	require.Equal(t, Subnets(), int(bigIntSubnetsCount.Uint64()))

	bigInst := new(big.Int)
	for i := 0; i < Subnets()*2; i++ {
		var cid spectypes.CommitteeID
		if _, err := rand.Read(cid[:]); err != nil {
			t.Fatal(err)
		}

		// Get result from CommitteeSubnet
		expected := CommitteeSubnet(cid)

		// Get result from SetCommitteeSubnet
		SetCommitteeSubnet(bigInst, cid)
		actual := bigInst.Uint64()

		require.Equal(t, expected, actual)
	}
}
