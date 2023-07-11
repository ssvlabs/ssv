package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/localevents"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type Task interface {
	GetEventType() string
	Execute() error
}
type RemoteTask struct {
	EventType string
	Edh       *EventDataHandler
	Ev        ethtypes.Log
	Shares    []*types.SSVShare
}

func NewRemoteTask(EventType string, Edh *EventDataHandler, Ev ethtypes.Log, Shares []*types.SSVShare) Task {
	return &RemoteTask{EventType, Edh, Ev, Shares}
}

func (t RemoteTask) Execute() error {
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
		t.Edh.logger.Info("liquidating cluster", fields.ClusterIndex(clusterLiquidatedEvent.Cluster))
		return t.Edh.taskExecutor.LiquidateCluster(clusterLiquidatedEvent, t.Shares)
	case ClusterReactivated:
		clusterReactivatedEvent, err := t.Edh.filterer.ParseClusterReactivated(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("reactivating cluster", fields.ClusterIndex(clusterReactivatedEvent.Cluster))
		return t.Edh.taskExecutor.ReactivateCluster(clusterReactivatedEvent, t.Shares)
	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := t.Edh.filterer.ParseFeeRecipientAddressUpdated(t.Ev)
		if err != nil {
			return err
		}
		t.Edh.logger.Info("updating recipient address", fields.Owner(feeRecipientAddressUpdatedEvent.Owner))
		return t.Edh.taskExecutor.UpdateFeeRecipient(feeRecipientAddressUpdatedEvent)
	default:
		return nil
	}
}

func (t RemoteTask) GetEventType() string { return t.EventType }

type LocalTask struct {
	EventType string
	Edh       *EventDataHandler
	Ev        *localevents.Event
	Shares    []*types.SSVShare
}

func NewLocalTask(EventType string, Edh *EventDataHandler, localEvent *localevents.Event, Shares []*types.SSVShare) Task {
	return &LocalTask{EventType, Edh, localEvent, Shares}
}

func (t LocalTask) Execute() error {
	switch t.EventType {
	case ValidatorAdded:
		e := t.Ev.Data.(contract.ContractValidatorAdded)
		t.Edh.logger.Info("starting validator", fields.PubKey(e.PublicKey))
		return t.Edh.taskExecutor.AddValidator(&e)
	case ValidatorRemoved:
		e := t.Ev.Data.(contract.ContractValidatorRemoved)
		t.Edh.logger.Info("stopping validator", fields.PubKey(e.PublicKey))
		return t.Edh.taskExecutor.RemoveValidator(&e)
	case ClusterLiquidated:
		e := t.Ev.Data.(contract.ContractClusterLiquidated)
		t.Edh.logger.Info("liquidating cluster", fields.ClusterIndex(e.Cluster))
		return t.Edh.taskExecutor.LiquidateCluster(&e, t.Shares)
	case ClusterReactivated:
		e := t.Ev.Data.(contract.ContractClusterReactivated)
		t.Edh.logger.Info("reactivating cluster", fields.ClusterIndex(e.Cluster))
		return t.Edh.taskExecutor.ReactivateCluster(&e, t.Shares)
	case FeeRecipientAddressUpdated:
		e := t.Ev.Data.(contract.ContractFeeRecipientAddressUpdated)
		t.Edh.logger.Info("updating recipient address", fields.Owner(e.Owner))
		return t.Edh.taskExecutor.UpdateFeeRecipient(&e)
	default:
		return nil
	}
}

func (t LocalTask) GetEventType() string { return t.EventType }
