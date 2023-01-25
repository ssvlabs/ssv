package tests

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network"
	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

type Scenario struct {
	Name                string
	Nodes               map[spectypes.OperatorID]network.P2PNetwork
	ValidationFunctions map[spectypes.OperatorID][]func(*protocolstorage.StoredInstance) error
}
