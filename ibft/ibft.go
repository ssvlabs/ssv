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
	state          *types.State
	network        types.Networker
	implementation types.Implementor
	params         *types.Params

	// messages
	prePrepareMessages []*types.Message
	prepareMessages    []*types.Message
	commitMessages     []*types.Message

	// flags
	started bool
}

func New(network types.Networker, implementation types.Implementor, params *types.Params) *iBFTInstance {
	return &iBFTInstance{
		state:              &types.State{},
		network:            network,
		implementation:     implementation,
		params:             params,
		started:            false,
		prePrepareMessages: make([]*types.Message, 0),
		prepareMessages:    make([]*types.Message, 0),
		commitMessages:     make([]*types.Message, 0),
	}
}

func (i *iBFTInstance) Start(lambda interface{}, inputValue interface{}) error {
	i.state.Lambda = lambda
	i.state.InputValue = inputValue

	if i.IsLeader() {
		if err := i.network.Broadcast(i.implementation.NewPrePrepareMsg(i.state)); err != nil {
			return err
		}
	}
	i.started = true
	i.roundChangeAfter(i.params.RoundChangeDuration)
	return nil
}

func (i *iBFTInstance) IsLeader() bool {
	return i.implementation.IsLeader(i.state)
}

func (i *iBFTInstance) roundChangeAfter(duration int64) {

}
