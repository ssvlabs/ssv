package commons

import (
	crand "crypto/rand"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func TestCommitteeSubnet(t *testing.T) {
	require.Equal(t, SubnetsCount, int(bigIntSubnetsCount.Uint64()))

	bigInst := new(big.Int)
	for i := 0; i < 2*SubnetsCount; i++ {
		var cid spectypes.CommitteeID
		if _, err := crand.Read(cid[:]); err != nil {
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

func TestSubnetsParsing(t *testing.T) {
	subtests := []struct {
		name        string
		str         string
		shouldError bool
	}{
		{
			"all subnets",
			"0xffffffffffffffffffffffffffffffff",
			false,
		},
		{
			"partial subnets",
			"0x57b080fffd743d9878dc41a184ab160a",
			false,
		},
		{
			"wrong size",
			"57b080fffd743d9878dc41a184ab1600",
			false,
		},
		{
			"invalid",
			"xxx",
			true,
		},
	}

	for _, subtest := range subtests {
		subtest := subtest
		t.Run(subtest.name, func(t *testing.T) {
			s, err := FromString(subtest.str)
			if subtest.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, strings.Replace(subtest.str, "0x", "", 1), s.String())
			}
		})
	}
}

func TestSharedSubnets(t *testing.T) {
	s1, err := FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	s2, err := FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	var expectedShared []int
	for subnet, val := range s2 {
		if val > 0 {
			expectedShared = append(expectedShared, subnet)
		}
	}
	shared := SharedSubnets(s1, s2, 0)
	require.Equal(t, expectedShared, shared)
}

func TestDiffSubnets(t *testing.T) {
	s1, err := FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	s2, err := FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	diff := DiffSubnets(s1, s2)
	require.Len(t, diff, 128-62)
}
