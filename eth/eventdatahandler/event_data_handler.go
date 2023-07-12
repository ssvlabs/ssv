package eventdatahandler

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/bloxapp/ssv-spec/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdb"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/localevents"
	"github.com/bloxapp/ssv/eth/sharemap"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/registry/storage"
)

type EventDataHandler struct {
	eventDB                    eventDB
	taskExecutor               TaskExecutor
	abi                        *ethabi.ABI
	shareMap                   *sharemap.ShareMap
	filterer                   *contract.ContractFilterer
	operatorData               *storage.OperatorData
	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	keyManager                 types.KeyManager
	beacon                     beaconprotocol.BeaconNode
	storageMap                 *qbftstorage.QBFTStores
	fullNode                   bool
	logger                     *zap.Logger
	metrics                    metrics
}

type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

func New(
	eventDB eventDB,
	client *executionclient.ExecutionClient,
	taskExecutor TaskExecutor,
	operatorData *storage.OperatorData,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	keyManager types.KeyManager,
	beacon beaconprotocol.BeaconNode,
	storageMap *qbftstorage.QBFTStores,
	opts ...Option,
) (*EventDataHandler, error) {
	abi, err := contract.ContractMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("get contract ABI: %w", err)
	}

	filterer, err := client.Filterer()
	if err != nil {
		return nil, fmt.Errorf("create contract filterer: %w", err)
	}

	edh := &EventDataHandler{
		eventDB:                    eventDB,
		taskExecutor:               taskExecutor,
		abi:                        abi,
		filterer:                   filterer,
		operatorData:               operatorData,
		shareEncryptionKeyProvider: shareEncryptionKeyProvider,
		keyManager:                 keyManager,
		beacon:                     beacon,
		storageMap:                 storageMap,
		logger:                     zap.NewNop(),
		metrics:                    nopMetrics{},
		shareMap:                   sharemap.New(),
	}

	for _, opt := range opts {
		opt(edh)
	}

	return edh, nil
}

func (edh *EventDataHandler) HandleBlockEventsStream(blockEventsCh <-chan eventbatcher.BlockEvents, executeTasks bool) (uint64, error) {
	var lastProcessedBlock uint64

	for blockEvents := range blockEventsCh {
		logger := edh.logger.With(fields.BlockNumber(blockEvents.BlockNumber))

		logger.Info("processing block events")
		tasks, err := edh.processBlockEvents(blockEvents)
		if err != nil {
			return 0, fmt.Errorf("process block events: %w", err)
		}

		lastProcessedBlock = blockEvents.BlockNumber

		logger.Info("processed block events")

		if !executeTasks || len(tasks) == 0 {
			continue
		}

		logger.Info("executing tasks", fields.Count(len(tasks)))

		// TODO:
		// find and remove opposite tasks (start-stop, stop-start, liquidate-reactivate, reactivate-liquidate)
		// find superseding tasks and remove superseded ones (updateFee-updateFee)
		for _, task := range tasks {
			logger = logger.With(zap.String("task_type", fmt.Sprintf("%T", task)))
			logger.Debug("going to execute task")
			if err := task.Execute(); err != nil {
				// TODO: We log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				logger.Error("failed to execute task", zap.Error(err))
			} else {
				logger.Debug("executed task")
			}
		}

		logger.Info("task execution finished", fields.Count(len(tasks)))
	}

	return lastProcessedBlock, nil
}

func (edh *EventDataHandler) processBlockEvents(blockEvents eventbatcher.BlockEvents) ([]Task, error) {
	txn := edh.eventDB.RWTxn()
	defer txn.Discard()

	var tasks []Task
	for _, event := range blockEvents.Events {
		task, err := edh.processEvent(txn, event)
		if err != nil {
			return nil, err
		}

		if task != nil {
			tasks = append(tasks, task)
		}
	}

	if err := txn.SetLastProcessedBlock(new(big.Int).SetUint64(blockEvents.BlockNumber)); err != nil {
		return nil, fmt.Errorf("set last processed block: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tasks, nil
}

func (edh *EventDataHandler) processEvent(txn eventdb.RW, event ethtypes.Log) (Task, error) {
	abiEvent, err := edh.abi.EventByID(event.Topics[0])
	if err != nil {
		edh.logger.Error("failed to find event by ID", zap.String("hash", event.Topics[0].String()))
		return nil, nil
	}

	switch abiEvent.Name {
	case OperatorAdded:
		operatorAddedEvent, err := edh.filterer.ParseOperatorAdded(event)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, fmt.Errorf("parse OperatorAdded: %w", err)
		}

		if err := edh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		edh.metrics.EventProcessed(abiEvent.Name)
		return nil, nil

	case OperatorRemoved:
		operatorRemovedEvent, err := edh.filterer.ParseOperatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorRemoved: %w", err)
		}

		if err := edh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		edh.metrics.EventProcessed(abiEvent.Name)
		return nil, nil

	case ValidatorAdded:
		validatorAddedEvent, err := edh.filterer.ParseValidatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorAdded: %w", err)
		}

		if err := edh.handleValidatorAdded(txn, validatorAddedEvent); err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		task := NewAddValidatorTask(edh.taskExecutor, validatorAddedEvent.PublicKey)

		edh.metrics.EventProcessed(abiEvent.Name)
		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := edh.filterer.ParseValidatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorRemoved: %w", err)
		}

		if err := edh.handleValidatorRemoved(txn, validatorRemovedEvent); err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		task := NewRemoveValidatorTask(edh.taskExecutor, validatorRemovedEvent.PublicKey)

		edh.metrics.EventProcessed(abiEvent.Name)
		return task, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := edh.filterer.ParseClusterLiquidated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterLiquidated: %w", err)
		}

		sharesToLiquidate, err := edh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		task := NewLiquidateClusterTask(edh.taskExecutor, clusterLiquidatedEvent.Owner, clusterLiquidatedEvent.OperatorIds, sharesToLiquidate)

		edh.metrics.EventProcessed(abiEvent.Name)
		return task, nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := edh.filterer.ParseClusterReactivated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterReactivated: %w", err)
		}

		sharesToEnable, err := edh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		task := NewReactivateClusterTask(edh.taskExecutor, clusterReactivatedEvent.Owner, clusterReactivatedEvent.OperatorIds, sharesToEnable)

		edh.metrics.EventProcessed(abiEvent.Name)
		return task, nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := edh.filterer.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			return nil, fmt.Errorf("parse FeeRecipientAddressUpdated: %w", err)
		}

		updated, err := edh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		if !updated {
			edh.metrics.EventProcessed(abiEvent.Name)
			return nil, nil
		}

		task := NewFeeRecipientTask(edh.taskExecutor, feeRecipientAddressUpdatedEvent.Owner, feeRecipientAddressUpdatedEvent.RecipientAddress)

		edh.metrics.EventProcessed(abiEvent.Name)
		return task, nil

	default:
		edh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}

func (edh *EventDataHandler) HandleLocalEvents(localEvents []localevents.Event) error {
	txn := edh.eventDB.RWTxn()
	defer txn.Discard()

	for _, event := range localEvents {
		if err := edh.processLocalEvent(txn, event); err != nil {
			return fmt.Errorf("process local event: %w", err)
		}
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (edh *EventDataHandler) processLocalEvent(txn eventdb.RW, event localevents.Event) error {
	switch event.Name {
	case OperatorAdded:
		data := event.Data.(contract.ContractOperatorAdded)
		if err := edh.handleOperatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorAdded: %w", err)
		}
		return nil
	case OperatorRemoved:
		data := event.Data.(contract.ContractOperatorRemoved)
		if err := edh.handleOperatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorRemoved: %w", err)
		}
		return nil
	case ValidatorAdded:
		data := event.Data.(contract.ContractValidatorAdded)
		if err := edh.handleValidatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		return nil
	case ValidatorRemoved:
		data := event.Data.(contract.ContractValidatorRemoved)
		if err := edh.handleValidatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		return nil
	case ClusterLiquidated:
		data := event.Data.(contract.ContractClusterLiquidated)
		_, err := edh.handleClusterLiquidated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterLiquidated: %w", err)
		}
		return nil
	case ClusterReactivated:
		data := event.Data.(contract.ContractClusterReactivated)
		_, err := edh.handleClusterReactivated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterReactivated: %w", err)
		}
		return nil
	case FeeRecipientAddressUpdated:
		data := event.Data.(contract.ContractFeeRecipientAddressUpdated)
		_, err := edh.handleFeeRecipientAddressUpdated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}
		return nil
	default:
		edh.logger.Warn("unknown local event name", fields.Name(event.Name))
		return nil
	}
}
