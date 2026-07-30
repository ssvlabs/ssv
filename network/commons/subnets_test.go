package commons

import (
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

func BenchmarkBooleCommitteeSubnet(b *testing.B) {
	committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BooleCommitteeSubnet(committee)
	}
}

func BenchmarkAlanCommitteeSubnet(b *testing.B) {
	cid := [32]byte{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AlanCommitteeSubnet(cid)
	}
}

func TestAlanCommitteeSubnet(t *testing.T) {
	require.Equal(t, SubnetsCount, int(bigIntSubnetsCount.Uint64()))

	bigInst := new(big.Int)
	for i := 0; i < 2*SubnetsCount; i++ {
		var cid spectypes.CommitteeID
		if _, err := crand.Read(cid[:]); err != nil {
			t.Fatal(err)
		}

		actual := AlanCommitteeSubnet(cid)

		// Cross-check against the reference big.Int math the memoized accessor must match.
		bigInst.SetBytes(cid[:])
		bigInst.Mod(bigInst, bigIntSubnetsCount)
		expected := bigInst.Uint64()

		require.Equal(t, expected, actual)
	}
}

func TestBooleCommitteeSubnet(t *testing.T) {
	require.Equal(t, SubnetsCount, int(bigIntSubnetsCount.Uint64()))

	t.Run("Boole", func(t *testing.T) {
		operators := []spectypes.OperatorID{
			1, // sha256(0100000000000000)=7c9fa136d4413fa6173637e883b6998d32e1d675f88cddff9dcbcf331820f4b8
			2, // sha256(0200000000000000)=d86e8112f3c4c4442126f8e9f44f16867da487f29052bf91b810457db34209a4
			3, // sha256(0300000000000000)=35be322d094f9d154a8aba4733b8497f180353bd7ae7b0a15f90b586b549f28b
			4, // sha256(0400000000000000)=f0a0278e4372459cca6159cd5e71cfee638302a7b9ca9b05c34181ac0a65ac5d
		}

		actual := BooleCommitteeSubnet(operators)

		hashes := make([][32]byte, 0, len(operators))
		for _, operator := range operators {
			var operatorBytes [8]byte
			binary.LittleEndian.PutUint64(operatorBytes[:], operator)
			hash := sha256.Sum256(operatorBytes[:])

			t.Logf("sha256(%x)=%x", operatorBytes, hash)
			hashes = append(hashes, hash)
		}

		lowestHash := hashes[2] // pre-calculated (lowest hash is 35be322d094f9d154a8aba4733b8497f180353bd7ae7b0a15f90b586b549f28b)
		expected := new(big.Int).Mod(new(big.Int).SetBytes(lowestHash[:]), bigIntSubnetsCount).Uint64()

		require.Equal(t, expected, actual)
	})

	t.Run("Alan", func(t *testing.T) {
		committeeID := spectypes.CommitteeID(bytes.Repeat([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}, 4))

		actual := AlanCommitteeSubnet(committeeID)
		expected := uint64(committeeID[31] % 128) // 0xef % 128 == 0xef % 0x80 == 0x6f

		require.Equal(t, expected, actual)
	})
}

func TestParseTopicSubnet(t *testing.T) {
	tests := []struct {
		name           string
		topic          string
		expectedSubnet uint64
		expectedBoole  bool
		expectedErr    bool
	}{
		{
			name:           "alan topic",
			topic:          GetTopicFullName(SubnetTopicID(12)),
			expectedSubnet: 12,
			expectedBoole:  false,
		},
		{
			name:           "boole topic",
			topic:          BooleTopic("mainnet", 42),
			expectedSubnet: 42,
			expectedBoole:  true,
		},
		{
			name:        "invalid alan subnet",
			topic:       "ssv.v2.not-a-subnet",
			expectedErr: true,
		},
		{
			name:        "invalid boole fork segment",
			topic:       "/ssv/mainnet/alan/42",
			expectedErr: true,
		},
		{
			name:        "missing boole subnet",
			topic:       "/ssv/mainnet/boole/",
			expectedErr: true,
		},
		{
			name:        "invalid topic root",
			topic:       "/other/mainnet/boole/42",
			expectedErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subnet, boole, err := ParseTopicSubnet(test.topic)
			if test.expectedErr {
				require.Error(t, err)
				require.False(t, boole)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.expectedSubnet, subnet)
			require.Equal(t, test.expectedBoole, boole)
		})
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

// TestTopics covers the four Boole-transition phases the pubsub startup whitelist must track in
// lockstep with per-slot subscription derivation (network/p2p's subscribedSubnetsForCurrentEpoch):
// pre-fork steady state, the prior window, the subsequent window (the bug this guards against —
// a node started in this window must still whitelist Alan topics for late pre-fork committee
// traffic), and well post-fork.
func TestTopics(t *testing.T) {
	booleCfg := func(booleEpoch phase0.Epoch) *networkconfig.Network {
		cfg := *networkconfig.TestNetwork
		beaconCfg := *networkconfig.TestNetwork.Beacon
		ssvCfg := *networkconfig.TestNetwork.SSV
		ssvCfg.Forks.Boole = booleEpoch
		cfg.Beacon = &beaconCfg
		cfg.SSV = &ssvCfg
		return &cfg
	}

	alanTopicsSet := func() map[string]struct{} {
		set := make(map[string]struct{}, SubnetsCount)
		for subnet := uint64(0); subnet < SubnetsCount; subnet++ {
			set[GetTopicFullName(SubnetTopicID(subnet))] = struct{}{}
		}
		return set
	}
	booleTopicsSet := func(name string) map[string]struct{} {
		set := make(map[string]struct{}, SubnetsCount)
		for subnet := uint64(0); subnet < SubnetsCount; subnet++ {
			set[BooleTopic(name, subnet)] = struct{}{}
		}
		return set
	}

	toSet := func(topics []string) map[string]struct{} {
		set := make(map[string]struct{}, len(topics))
		for _, topic := range topics {
			set[topic] = struct{}{}
		}
		return set
	}

	cur := networkconfig.TestNetwork.EstimatedCurrentEpoch()

	t.Run("pre-fork steady state includes Alan and Boole", func(t *testing.T) {
		cfg := booleCfg(math.MaxUint64) // no fork scheduled
		slot := cfg.EstimatedCurrentSlot()
		require.False(t, cfg.BooleForkAtSlot(slot))
		require.False(t, cfg.InBooleTransitionWindow(slot), "far from the fork, expected no transition window")

		got := toSet(Topics(cfg, slot))
		want := alanTopicsSet()
		for topic := range booleTopicsSet(cfg.SSV.Name) {
			want[topic] = struct{}{}
		}
		require.Equal(t, want, got)
	})

	t.Run("prior window includes Alan and Boole", func(t *testing.T) {
		cfg := booleCfg(cur + 2)
		slot := cfg.FirstSlotAtEpoch(cur + 1) // last pre-fork epoch, inside the 1-epoch prior window
		require.False(t, cfg.BooleForkAtSlot(slot))
		require.True(t, cfg.InBooleTransitionWindow(slot), "expected slot inside the prior window")

		got := toSet(Topics(cfg, slot))
		want := alanTopicsSet()
		for topic := range booleTopicsSet(cfg.SSV.Name) {
			want[topic] = struct{}{}
		}
		require.Equal(t, want, got)
	})

	t.Run("subsequent window includes Alan and Boole (bug fix)", func(t *testing.T) {
		cfg := booleCfg(cur + 1)
		slot := cfg.FirstSlotAtEpoch(cur + 1) // fork slot itself, start of the subsequent window
		require.True(t, cfg.BooleForkAtSlot(slot))
		require.True(t, cfg.InBooleTransitionWindow(slot), "expected slot inside the subsequent window")

		got := toSet(Topics(cfg, slot))
		want := alanTopicsSet()
		for topic := range booleTopicsSet(cfg.SSV.Name) {
			want[topic] = struct{}{}
		}
		require.Equal(t, want, got)
	})

	t.Run("first slot past the subsequent window is Boole-only", func(t *testing.T) {
		cfg := booleCfg(cur + 1)
		// One slot past the window end [F, F+SlotsPerEpoch+2) — the boundary that closes Alan.
		slot := cfg.FirstSlotAtEpoch(cur+1) + phase0.Slot(cfg.SlotsPerEpoch) + 2
		require.True(t, cfg.BooleForkAtSlot(slot))
		require.False(t, cfg.InBooleTransitionWindow(slot), "expected slot just past the subsequent window")

		got := toSet(Topics(cfg, slot))
		want := booleTopicsSet(cfg.SSV.Name)
		require.Equal(t, want, got)
	})

	t.Run("well post-fork is Boole-only", func(t *testing.T) {
		cfg := booleCfg(0)
		slot := cfg.EstimatedCurrentSlot()
		require.True(t, cfg.BooleForkAtSlot(slot))
		require.False(t, cfg.InBooleTransitionWindow(slot), "well past the fork, expected no transition window")

		got := toSet(Topics(cfg, slot))
		want := booleTopicsSet(cfg.SSV.Name)
		require.Equal(t, want, got)
	})
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
