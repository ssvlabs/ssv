package ibft

import (
	"github.com/bloxapp/ssv/ibft/types"
	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
)

func Place() {
	att := eth.Attestation{}
	att.String()

	blk := eth.BeaconBlock{}
	blk.String()
}

type iBFTInstance struct {
	state   *types.State
	network types.Networker

	// messages
	prePrepareMessages []*types.Message
	prepareMessages    []*types.Message
	commitMessages     []*types.Message
}

func Start(network types.Networker, lambda interface{}, inputValue interface{}) *iBFTInstance {
	ret := &iBFTInstance{
		state: &types.State{
			Lambda:        lambda,
			Round:         0,
			PreparedRound: 0,
			PreparedValue: nil,
			InputValue:    inputValue,
		},
		network:            network,
		prePrepareMessages: make([]*types.Message, 0),
		prepareMessages:    make([]*types.Message, 0),
		commitMessages:     make([]*types.Message, 0),
	}

	return ret
}
