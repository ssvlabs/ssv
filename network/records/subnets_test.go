package records

import (
	crand "crypto/rand"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/network/commons"
)

func Test_SubnetsEntry(t *testing.T) {
	SubnetsCount := 128
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	sk, err := commons.ECDSAPrivFromInterface(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	subnets := make([]byte, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			subnets[i] = 1
		} else {
			subnets[i] = 0
		}
	}
	require.NoError(t, SetSubnetsEntry(node, subnets))
	t.Log("ENR with subnets :", node.Node().String())

	subnetsFromEnr, err := GetSubnetsEntry(node.Node().Record())
	require.NoError(t, err)
	require.Len(t, subnetsFromEnr, SubnetsCount)
	for i := 0; i < SubnetsCount; i++ {
		if i%4 == 0 {
			require.Equal(t, byte(1), subnetsFromEnr[i])
		} else {
			require.Equal(t, byte(0), subnetsFromEnr[i])
		}
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
			s, err := Subnets{}.FromString(subtest.str)
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
	s1, err := Subnets{}.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	s2, err := Subnets{}.FromString("0x57b080fffd743d9878dc41a184ab160a")
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
	s1, err := Subnets{}.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	s2, err := Subnets{}.FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	diff := DiffSubnets(s1, s2)
	require.Len(t, diff, 128-62)
}
