package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth/contract"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type eventHandlers interface {
	HandleOperatorAdded(event *contract.ContractOperatorAdded) error
	HandleOperatorRemoved(event *contract.ContractOperatorRemoved) error
	HandleValidatorAdded(event *contract.ContractValidatorAdded) error
	HandleValidatorRemoved(event *contract.ContractValidatorRemoved) error
	HandleClusterLiquidated(event *contract.ContractClusterLiquidated) ([]*ssvtypes.SSVShare, error)
	HandleClusterReactivated(event *contract.ContractClusterReactivated) ([]*ssvtypes.SSVShare, error)
	HandleFeeRecipientAddressUpdated(event *contract.ContractFeeRecipientAddressUpdated) error
}
