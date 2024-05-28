package tests

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type TestingBeaconNodeWrapped struct {
	beacon.BeaconNode
	bn *spectestingutils.TestingBeaconNode
}

func (bn *TestingBeaconNodeWrapped) SetSyncCommitteeAggregatorRootHexes(roots map[string]bool) {
	bn.bn.SetSyncCommitteeAggregatorRootHexes(roots)
}

func (bn *TestingBeaconNodeWrapped) GetBroadcastedRoots() []phase0.Root {
	return bn.bn.BroadcastedRoots
}

func (bn *TestingBeaconNodeWrapped) GetBeaconNode() *spectestingutils.TestingBeaconNode {
	return bn.bn
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &TestingBeaconNodeWrapped{}
	bnw.bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
