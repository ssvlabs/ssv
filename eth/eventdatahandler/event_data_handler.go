package eventdatahandler

import (
	"crypto/rsa"
	"fmt"
	"log"
	"math/big"

	"github.com/bloxapp/ssv-spec/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdb"
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

// TODO: try to reduce amount of input parameters

func New(
	eventDB eventDB,
	taskExecutor TaskExecutor,
	operatorData *storage.OperatorData,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	keyManager types.KeyManager,
	beacon beaconprotocol.BeaconNode,
	storageMap *qbftstorage.QBFTStores,
	opts ...Option,
) *EventDataHandler {
	abi, err := contract.ContractMetaData.GetAbi()
	if err != nil {
		log.Fatal(err) // TODO: handle
	}

	// TODO: zero values don't look well, think of a workaround, perhaps pass Eth1Client with Filterer method to New
	filterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
	if err != nil {
		panic(err) // TODO: handle
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

	return edh
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

		logger = logger.With(fields.Count(len(tasks)))
		logger.Info("executing tasks")

		// TODO:
		// 1) find and remove opposite tasks (start-stop, stop-start, liquidate-reactivate, reactivate-liquidate)
		// 2) find superseding tasks and remove superseded ones (updateFee-updateFee)
		for _, task := range tasks {
			edh.logger.Debug("going to execute task") // TODO: add more task details
			if err := task(); err != nil {
				// TODO: We log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				edh.logger.Error("failed to execute task", zap.Error(err))
			} else {
				edh.logger.Debug("executed task") // TODO: add more task details
			}
		}

		logger.Info("task execution finished")
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
			return nil, fmt.Errorf("parse OperatorAdded: %w", err)
		}

		if err := edh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		return nil, nil

	case OperatorRemoved:
		operatorRemovedEvent, err := edh.filterer.ParseOperatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorRemoved: %w", err)
		}

		if err := edh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		return nil, nil

	case ValidatorAdded:
		validatorAddedEvent, err := edh.filterer.ParseValidatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorAdded: %w", err)
		}

		if err := edh.handleValidatorAdded(txn, validatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		task := func() error {
			if err := edh.taskExecutor.AddValidator(validatorAddedEvent); err != nil {
				return fmt.Errorf("add validator: %w", err)
			}

			return nil
		}

		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := edh.filterer.ParseValidatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorRemoved: %w", err)
		}

		if err := edh.handleValidatorRemoved(txn, validatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		task := func() error {
			if err := edh.taskExecutor.RemoveValidator(validatorRemovedEvent); err != nil {
				return fmt.Errorf("remove validator: %w", err)
			}

			return nil
		}

		return task, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := edh.filterer.ParseClusterLiquidated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterLiquidated: %w", err)
		}

		sharesToLiquidate, err := edh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		task := func() error {
			if err := edh.taskExecutor.LiquidateCluster(clusterLiquidatedEvent, sharesToLiquidate); err != nil {
				return fmt.Errorf("liquidate cluster: %w", err)
			}

			return nil
		}

		return task, nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := edh.filterer.ParseClusterReactivated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterReactivated: %w", err)
		}

		sharesToEnable, err := edh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		task := func() error {
			if err := edh.taskExecutor.ReactivateCluster(clusterReactivatedEvent, sharesToEnable); err != nil {
				return fmt.Errorf("reactivate cluster: %w", err)
			}

			return nil
		}

		return task, nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := edh.filterer.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			return nil, fmt.Errorf("parse FeeRecipientAddressUpdated: %w", err)
		}

		updated, err := edh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		task := func() error {
			if !updated {
				return nil
			}

			if err := edh.taskExecutor.UpdateFeeRecipient(feeRecipientAddressUpdatedEvent); err != nil {
				return fmt.Errorf("update fee recipient: %w", err)
			}

			return nil
		}

		return task, nil

	default:
		edh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}
