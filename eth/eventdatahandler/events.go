package eventdatahandler

import (
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth/contract"
)

// Event names
const (
	OperatorAdded              = "OperatorAdded"
	OperatorRemoved            = "OperatorRemoved"
	ValidatorAdded             = "ValidatorAdded"
	ValidatorRemoved           = "ValidatorRemoved"
	ClusterLiquidated          = "ClusterLiquidated"
	ClusterReactivated         = "ClusterReactivated"
	FeeRecipientAddressUpdated = "FeeRecipientAddressUpdated"
)

type eventFilterer interface {
	ParseOperatorAdded(log ethtypes.Log) (*contract.ContractOperatorAdded, error)
	ParseOperatorRemoved(log ethtypes.Log) (*contract.ContractOperatorRemoved, error)
	ParseValidatorAdded(log ethtypes.Log) (*contract.ContractValidatorAdded, error)
	ParseValidatorRemoved(log ethtypes.Log) (*contract.ContractValidatorRemoved, error)
	ParseClusterLiquidated(log ethtypes.Log) (*contract.ContractClusterLiquidated, error)
	ParseClusterReactivated(log ethtypes.Log) (*contract.ContractClusterReactivated, error)
	ParseFeeRecipientAddressUpdated(log ethtypes.Log) (*contract.ContractFeeRecipientAddressUpdated, error)
}

type ethEventGetter interface {
	EventByID(topic common.Hash) (*ethabi.Event, error)
}
