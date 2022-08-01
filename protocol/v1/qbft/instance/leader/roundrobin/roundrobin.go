package roundrobin

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
)

// RoundRobin robin leader selection will always return the same leader
type RoundRobin struct {
	share *beaconprotocol.Share
	state *qbftprotocol.State
}

// New returns a new RoundRobin instance.
func New(share *beaconprotocol.Share, state *qbftprotocol.State) *RoundRobin {
	return &RoundRobin{
		share: share,
		state: state,
	}
}

// Calculate returns the current leader
func (rr *RoundRobin) Calculate(round uint64) uint64 {
	mappedState := &specqbft.State{
		Share: &spectypes.Share{
			Committee: mapCommittee(rr.share),
		},
		Height: rr.state.GetHeight(),
	}

	return uint64(specqbft.RoundRobinProposer(mappedState, specqbft.Round(round)))
}

func mapCommittee(share *beaconprotocol.Share) []*spectypes.Operator {
	mappedCommittee := make([]*spectypes.Operator, 0)
	for _, operatorID := range share.OperatorIds {
		node := share.Committee[spectypes.OperatorID(operatorID)]
		mappedCommittee = append(mappedCommittee, &spectypes.Operator{
			OperatorID: spectypes.OperatorID(node.IbftID),
			PubKey:     node.Pk,
		})
	}

	return mappedCommittee
}
