package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type TaskExecutor interface {
	AddValidator(*contract.ContractValidatorAdded) error
	RemoveValidator(*contract.ContractValidatorRemoved) error
	LiquidateCluster(*contract.ContractClusterLiquidated, []*types.SSVShare) error
	ReactivateCluster(*contract.ContractClusterReactivated, []*types.SSVShare) error
	UpdateFeeRecipient(*contract.ContractFeeRecipientAddressUpdated) error
}
