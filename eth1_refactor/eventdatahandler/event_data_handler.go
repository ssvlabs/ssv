package eventdatahandler

import (
	"fmt"
	"log"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1_refactor/contract"
	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
	"github.com/bloxapp/ssv/logging/fields"
)

type EventDataHandler struct {
	eventDB       eventDB
	taskExecutor  taskExecutor
	abi           *ethabi.ABI
	filterer      *contract.ContractFilterer
	eventHandlers eventHandlers

	logger  *zap.Logger
	metrics metrics
}

func NewEventDataHandler(eventDB eventDB, taskExecutor taskExecutor) *EventDataHandler {
	// Parse the contract's ABI
	abi, err := contract.ContractMetaData.GetAbi()
	if err != nil {
		log.Fatal(err) // TODO: handle
	}

	// TODO: zero values don't look well, think of a workaround, perhaps pass Eth1Client with Filterer method to NewEventDataHandler
	filterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	if err != nil {
		panic(err) // TODO: handle
	}

	return &EventDataHandler{
		eventDB:       eventDB,
		taskExecutor:  taskExecutor,
		abi:           abi,
		filterer:      filterer,
		logger:        zap.NewNop(),
		metrics:       nopMetrics{},
		eventHandlers: nil, // TODO: create nopHandler
	}
}

func (edh *EventDataHandler) HandleBlockEventsStream(blockEventsCh <-chan eventbatcher.BlockEvents) error {
	for blockEvents := range blockEventsCh {
		tasks, err := edh.processBlockEvents(blockEvents)
		if err != nil {
			return fmt.Errorf("process block events: %w", err)
		}

		if len(tasks) == 0 {
			continue
		}

		logger := edh.logger.With(fields.BlockNumber(blockEvents.BlockNumber))
		logger.Info("executing tasks")

		// TODO:
		// 1) find and remove opposite tasks (start-stop, stop-start, liquidate-reactivate, reactivate-liquidate)
		// 2) find superseding tasks and remove superseded ones (updateFee-updateFee)
		for _, task := range tasks {
			if err := task(); err != nil {
				return fmt.Errorf("execute task: %w", err)
			}
		}

		logger.Info("executed tasks")
	}

	return nil
}

func (edh *EventDataHandler) processBlockEvents(blockEvents eventbatcher.BlockEvents) ([]Task, error) {
	edh.eventDB.BeginTx()

	var tasks []Task
	for _, event := range blockEvents.Events {
		task, err := edh.processEvent(event)
		if err != nil {
			edh.eventDB.Rollback()
			return nil, err
		}

		if task != nil {
			tasks = append(tasks, task)
		}
	}

	edh.eventDB.EndTx()

	return tasks, nil
}

func (edh *EventDataHandler) processEvent(event ethtypes.Log) (Task, error) {
	abiEvent, err := edh.abi.EventByID(event.Topics[0])
	if err != nil {
		return nil, err
	}

	switch abiEvent.Name {
	case OperatorAdded:
		operatorAddedEvent, err := edh.filterer.ParseOperatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorAdded: %w", err)
		}

		if err := edh.eventHandlers.HandleOperatorAdded(operatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		return nil, nil

	case OperatorRemoved:
		operatorRemovedEvent, err := edh.filterer.ParseOperatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorRemoved: %w", err)
		}

		if err := edh.eventHandlers.HandleOperatorRemoved(operatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		return nil, nil

	case ValidatorAdded:
		validatorAddedEvent, err := edh.filterer.ParseValidatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorAdded: %w", err)
		}

		if err := edh.eventHandlers.HandleValidatorAdded(validatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		task := func() error {
			edh.logger.Info("starting validator", fields.PubKey(validatorAddedEvent.PublicKey)) // TODO: move logs to taskExecutor

			return edh.taskExecutor.AddValidator(validatorAddedEvent)
		}

		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := edh.filterer.ParseValidatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorRemoved: %w", err)
		}

		if err := edh.eventHandlers.HandleValidatorRemoved(validatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		task := func() error {
			edh.logger.Info("stopping validator", fields.PubKey(validatorRemovedEvent.PublicKey))

			return edh.taskExecutor.RemoveValidator(validatorRemovedEvent)
		}

		return task, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := edh.filterer.ParseClusterLiquidated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterLiquidated: %w", err)
		}

		sharesToLiquidate, err := edh.eventHandlers.HandleClusterLiquidated(clusterLiquidatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		task := func() error {
			edh.logger.Info("liquidating cluster", zap.Uint64("index", clusterLiquidatedEvent.Cluster.Index)) // TODO: add to fields package

			return edh.taskExecutor.LiquidateCluster(clusterLiquidatedEvent, sharesToLiquidate)
		}

		return task, nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := edh.filterer.ParseClusterReactivated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterReactivated: %w", err)
		}

		sharesToEnable, err := edh.eventHandlers.HandleClusterReactivated(clusterReactivatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		task := func() error {
			edh.logger.Info("reactivating cluster", zap.Uint64("index", clusterReactivatedEvent.Cluster.Index)) // TODO: add to fields package

			return edh.taskExecutor.ReactivateCluster(clusterReactivatedEvent, sharesToEnable)
		}

		return task, nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := edh.filterer.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			return nil, fmt.Errorf("parse FeeRecipientAddressUpdated: %w", err)
		}

		if err := edh.eventHandlers.HandleFeeRecipientAddressUpdated(feeRecipientAddressUpdatedEvent); err != nil {
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		task := func() error {
			edh.logger.Info("updating recipient address", zap.Stringer("owner", feeRecipientAddressUpdatedEvent.Owner)) // TODO: add to fields package

			return edh.taskExecutor.UpdateFeeRecipient(feeRecipientAddressUpdatedEvent)
		}

		return task, nil

	default:
		return nil, fmt.Errorf("unknown event name %q", abiEvent.Name)
	}
}
