package eventdatahandler

import (
	"fmt"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type Task struct {
	Edh    *EventDataHandler
	Event  interface{}
	Shares []*types.SSVShare
}

func NewTask(edh *EventDataHandler, event interface{}, shares []*types.SSVShare) *Task {
	// TODO: change event type from interface{}
	return &Task{Edh: edh, Event: event, Shares: shares}
}

func (t *Task) Execute() error {
	switch e := t.Event.(type) {
	case *contract.ContractValidatorAdded:
		t.Edh.logger.Info("starting validator", fields.PubKey(e.PublicKey))
		return t.Edh.taskExecutor.AddValidator(e)
	case *contract.ContractValidatorRemoved:
		t.Edh.logger.Info("stopping validator", fields.PubKey(e.PublicKey))
		return t.Edh.taskExecutor.RemoveValidator(e)
	case *contract.ContractClusterLiquidated:
		t.Edh.logger.Info("liquidating cluster", fields.ClusterIndex(e.Cluster))
		return t.Edh.taskExecutor.LiquidateCluster(e, t.Shares)
	case *contract.ContractClusterReactivated:
		t.Edh.logger.Info("reactivating cluster", fields.ClusterIndex(e.Cluster))
		return t.Edh.taskExecutor.ReactivateCluster(e, t.Shares)
	case *contract.ContractFeeRecipientAddressUpdated:
		t.Edh.logger.Info("updating recipient address", fields.Owner(e.Owner))
		return t.Edh.taskExecutor.UpdateFeeRecipient(e)
	default:
		return fmt.Errorf("failed to infer task type")
	}
}
