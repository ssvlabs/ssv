package qbft

import (
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

func NewTestingQBFTController(
	identifier []byte,
	share *spectypes.Share,
	config types.IConfig,
) *controller.Controller {
	return controller.NewController(
		identifier,
		share,
		spectypes.PrimusTestnet,
		config,
	)
}
