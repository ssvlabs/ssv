package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth1_refactor/contract"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type taskExecutor interface {
	AddValidator(*contract.ContractValidatorAdded) error
	RemoveValidator(*contract.ContractValidatorRemoved) error
	LiquidateCluster(*contract.ContractClusterLiquidated, []*ssvtypes.SSVShare) error
	ReactivateCluster(*contract.ContractClusterReactivated, []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(*contract.ContractFeeRecipientAddressUpdated) error
}
