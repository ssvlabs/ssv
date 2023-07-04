package eventdatahandler

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

type Task struct {
	EventType
	Edh    *EventDataHandler
	Ev     ethtypes.Log
	Shares []*types.SSVShare
}

func NewTask(EventType EventType, Edh *EventDataHandler, Ev ethtypes.Log, Shares []*types.SSVShare) *Task {
	return &Task{EventType, Edh, Ev, Shares}
}

func (t *Task) Execute() error {
	switch t.EventType {
	case ValidatorAdded:
		validatorAddedEvent, err := t.Edh.filterer.ParseValidatorAdded(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("starting validator", fields.PubKey(validatorAddedEvent.PublicKey))
		return t.Edh.taskExecutor.AddValidator(validatorAddedEvent)
	case ValidatorRemoved:
		validatorRemovedEvent, err := t.Edh.filterer.ParseValidatorRemoved(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("stopping validator", fields.PubKey(validatorRemovedEvent.PublicKey))
		return t.Edh.taskExecutor.RemoveValidator(validatorRemovedEvent)
	case ClusterLiquidated:
		clusterLiquidatedEvent, err := t.Edh.filterer.ParseClusterLiquidated(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("liquidating cluster", zap.Uint64("index", clusterLiquidatedEvent.Cluster.Index)) // TODO: add to fields package
		return t.Edh.taskExecutor.LiquidateCluster(clusterLiquidatedEvent, t.Shares)
	case ClusterReactivated:
		clusterReactivatedEvent, err := t.Edh.filterer.ParseClusterReactivated(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("reactivating cluster", zap.Uint64("index", clusterReactivatedEvent.Cluster.Index)) // TODO: add to fields package
		return t.Edh.taskExecutor.ReactivateCluster(clusterReactivatedEvent, t.Shares)
	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := t.Edh.filterer.ParseFeeRecipientAddressUpdated(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("updating recipient address", zap.Stringer("owner", feeRecipientAddressUpdatedEvent.Owner)) // TODO: add to fields package
		return t.Edh.taskExecutor.UpdateFeeRecipient(feeRecipientAddressUpdatedEvent)
	default:
		return nil
	}
}
