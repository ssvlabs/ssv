package validation

import (
	"math"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
)

// oldValidateTopicAtSlot recomputes the subnet from scratch (SHA-256 over the committee for
// Boole, big.Int mod for Alan) on every call, mirroring the pre-optimization implementation.
// It exists only in this test-only benchmark file to give an apples-to-apples before/after
// comparison against the memoized-subnet validateTopicAtSlot.
func oldValidateTopicAtSlot(netCfg *networkconfig.Network, committeeID spectypes.CommitteeID, committee []spectypes.OperatorID, topic string, slot phase0.Slot) error {
	var expectedTopic string
	if netCfg.BooleForkAtSlot(slot) {
		expectedTopic = commons.BooleTopic(netCfg.SSV.Name, commons.BooleCommitteeSubnet(committee))
	} else {
		expectedTopic = commons.GetTopicFullName(commons.CommitteeTopicID(committeeID)[0])
	}

	if expectedTopic != topic {
		e := ErrIncorrectTopic
		e.got = topic
		e.want = expectedTopic
		return e
	}

	return nil
}

func benchCommitteeInfoAndNetCfg(boolePostFork bool) (*messageValidator, CommitteeInfo, string, phase0.Slot) {
	ssvCfg := *networkconfig.TestNetwork.SSV
	if boolePostFork {
		ssvCfg.Forks = networkconfig.SSVForks{Boole: 0}
	} else {
		ssvCfg.Forks = networkconfig.SSVForks{Boole: math.MaxUint64}
	}
	netCfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &ssvCfg}

	committee := []spectypes.OperatorID{1, 2, 3, 4}
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], big.NewInt(12345).Bytes())

	booleSubnet := commons.BooleCommitteeSubnet(committee)
	alanSubnet := commons.AlanCommitteeSubnet(committeeID)
	committeeInfo := newCommitteeInfo(committeeID, committee, nil, booleSubnet, alanSubnet)

	slot := netCfg.FirstSlotAtEpoch(1)

	var topic string
	if boolePostFork {
		topic = commons.BooleTopic(netCfg.SSV.Name, booleSubnet)
	} else {
		topic = commons.GetTopicFullName(commons.CommitteeTopicID(committeeID)[0])
	}

	mv := &messageValidator{netCfg: netCfg}

	return mv, committeeInfo, topic, slot
}

// BenchmarkValidateTopicAtSlot_New_PostFork measures the memoized-subnet hot path (no hashing).
func BenchmarkValidateTopicAtSlot_New_PostFork(b *testing.B) {
	mv, committeeInfo, topic, slot := benchCommitteeInfoAndNetCfg(true)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := mv.validateTopicAtSlot(committeeInfo, topic, slot); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateTopicAtSlot_Old_PostFork measures the pre-optimization per-message SHA-256
// recompute, for comparison against BenchmarkValidateTopicAtSlot_New_PostFork.
func BenchmarkValidateTopicAtSlot_Old_PostFork(b *testing.B) {
	mv, committeeInfo, topic, slot := benchCommitteeInfoAndNetCfg(true)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := oldValidateTopicAtSlot(mv.netCfg, committeeInfo.committeeID, committeeInfo.committee, topic, slot); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateTopicAtSlot_New_PreFork measures the memoized-subnet hot path pre-fork.
func BenchmarkValidateTopicAtSlot_New_PreFork(b *testing.B) {
	mv, committeeInfo, topic, slot := benchCommitteeInfoAndNetCfg(false)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := mv.validateTopicAtSlot(committeeInfo, topic, slot); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateTopicAtSlot_Old_PreFork measures the pre-optimization big.Int-mod recompute
// pre-fork, for comparison against BenchmarkValidateTopicAtSlot_New_PreFork.
func BenchmarkValidateTopicAtSlot_Old_PreFork(b *testing.B) {
	mv, committeeInfo, topic, slot := benchCommitteeInfoAndNetCfg(false)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := oldValidateTopicAtSlot(mv.netCfg, committeeInfo.committeeID, committeeInfo.committee, topic, slot); err != nil {
			b.Fatal(err)
		}
	}
}

// Test_ValidateTopicAtSlot_NoSubnetRecomputeAllocs asserts the new hot path makes zero
// big.Int/sha256-driven allocations: the only allocations left come from formatting the topic
// string (fmt.Sprintf), which is identical work on both the old and new paths. We assert this by
// comparing against the old (pre-optimization) recompute, which pays for the same formatting
// plus the now-eliminated subnet computation - so new must allocate strictly less.
func Test_ValidateTopicAtSlot_NoSubnetRecomputeAllocs(t *testing.T) {
	mv, committeeInfo, topic, slot := benchCommitteeInfoAndNetCfg(true)

	newAllocs := testing.AllocsPerRun(100, func() {
		if err := mv.validateTopicAtSlot(committeeInfo, topic, slot); err != nil {
			t.Fatal(err)
		}
	})
	oldAllocs := testing.AllocsPerRun(100, func() {
		if err := oldValidateTopicAtSlot(mv.netCfg, committeeInfo.committeeID, committeeInfo.committee, topic, slot); err != nil {
			t.Fatal(err)
		}
	})

	require.Less(t, newAllocs, oldAllocs)
}
