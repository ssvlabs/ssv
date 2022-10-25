package qbft

import (
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	types2 "github.com/bloxapp/ssv/protocol/v2/types"
)

func NewTestingQBFTController(
	identifier []byte,
	share *types.Share,
	config types2.IConfig,
) *controller.Controller {
	return controller.NewController(
		identifier,
		share,
		types.PrimusTestnet,
		config,
	)
}
