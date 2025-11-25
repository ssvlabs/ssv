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
		t.Run(subtest.name, func(t *testing.T) {
			s, err := SubnetsFromString(subtest.str)
			if subtest.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, strings.Replace(subtest.str, "0x", "", 1), s.StringHex())
			}
		})
	}
}

func TestSharedSubnets(t *testing.T) {
	s1 := AllSubnets
	s2, err := SubnetsFromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	expectedShared := s2.SubnetList()
	shared := s1.SharedSubnets(s2)
	require.Equal(t, expectedShared, shared)
}

func TestDiffSubnets(t *testing.T) {
	s1 := AllSubnets
	s2, err := SubnetsFromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	added, removed := s1.DiffSubnets(s2)
	require.Equal(t, 128-62, added.ActiveCount()+removed.ActiveCount())
}

func TestSubnetsList(t *testing.T) {
	// Test Case 1: All subnets unset
	subnets := ZeroSubnets
	subnetList := subnets.SubnetList()
	require.Emptyf(t, subnetList, "Expected 0 subnets, got %d", len(subnetList))

	// Test Case 2: All subnets set
	subnets = AllSubnets
	subnetList = subnets.SubnetList()

	require.Lenf(t, subnetList, SubnetsCount, "Expected %d subnets, got %d", SubnetsCount, len(subnetList))
	for i := uint64(0); i < SubnetsCount; i++ {
		require.EqualValuesf(t, i, subnetList[i], "Expected subnet index %d, got %d", i, subnetList[i])
	}

	// Test Case 3: Random subnets set
	expected := []uint64{0, 15, 16, 31, 63, 64, 127}

	subnets = ZeroSubnets
	for _, v := range expected {
		subnets.Set(v)
	}

	subnetList = subnets.SubnetList()
	require.Lenf(t, subnetList, len(expected), "Expected %d subnets, got %d", len(expected), len(subnetList))
	for i, idx := range expected {
		require.Equalf(t, idx, subnetList[i], "At position %d: expected %d, got %d", i, idx, subnetList[i])
	}

	// Test Case 4: No subnets set
	subnets = ZeroSubnets
	subnetList = subnets.SubnetList()
	require.Emptyf(t, subnetList, "Expected 0 subnets, got %d", len(subnetList))

	// Test Case 5: Single subnet set
	subnets = ZeroSubnets
	subnets.Set(42)
	subnetList = subnets.SubnetList()
	require.Lenf(t, subnetList, 1, "Expected 1 subnet, got %d", len(subnetList))
	require.EqualValuesf(t, 42, subnetList[0], "Expected subnet [42], got %v", subnetList)

	// Test Case 6: Clearing subnets
	subnets = AllSubnets
	subnets.Clear(0)
	subnets.Clear(64)
	subnets.Clear(127)
	subnetList = subnets.SubnetList()
	require.Lenf(t, subnetList, SubnetsCount-3, "Expected %d subnets, got %d", SubnetsCount-3, len(subnetList))
	for _, idx := range []int{0, 64, 127} {
		require.NotContainsf(t, subnetList, idx, "Subnet %d should have been cleared", idx)
	}
}
