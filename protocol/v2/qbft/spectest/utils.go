package qbft

import (
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
)

func NewTestingQBFTController(
	identifier []byte,
	share *types.Share,
	config qbft.IConfig,
) *controller.Controller {
	return controller.NewController(
		identifier,
		share,
		types.PrimusTestnet,
		config,
	)
}
