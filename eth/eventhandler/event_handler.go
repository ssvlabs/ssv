// Package eventhandler provides support for handling registry contract events
// and persisting them to the database.
package eventhandler

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/localevents"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
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

var (
	// ErrInferiorBlock is returned when trying to process a block that is
	// not higher than the last processed block.
	ErrInferiorBlock = errors.New("block is not higher than the last processed block")
)

type taskExecutor interface {
	StartValidator(share *ssvtypes.SSVShare) error
	StopValidator(pubKey spectypes.ValidatorPK) error
	LiquidateCluster(owner ethcommon.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner ethcommon.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient ethcommon.Address) error
}

type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

type OperatorData interface {
	GetOperatorData() *storage.OperatorData
	SetOperatorData(*storage.OperatorData)
}

type EventHandler struct {
	nodeStorage                nodestorage.Storage
	taskExecutor               taskExecutor
	eventParser                eventparser.Parser
	domain                     spectypes.DomainType
	operatorData               OperatorData
	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	keyManager                 spectypes.KeyManager
	beacon                     beaconprotocol.BeaconNode
	storageMap                 *qbftstorage.QBFTStores

	fullNode bool
	logger   *zap.Logger
	metrics  metrics
}

func New(
	nodeStorage nodestorage.Storage,
	eventParser eventparser.Parser,
	taskExecutor taskExecutor,
	domain spectypes.DomainType,
	operatorData OperatorData,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	keyManager spectypes.KeyManager,
	beacon beaconprotocol.BeaconNode,
	storageMap *qbftstorage.QBFTStores,
	opts ...Option,
) (*EventHandler, error) {
	eh := &EventHandler{
		nodeStorage:                nodeStorage,
		taskExecutor:               taskExecutor,
		eventParser:                eventParser,
		domain:                     domain,
		operatorData:               operatorData,
		shareEncryptionKeyProvider: shareEncryptionKeyProvider,
		keyManager:                 keyManager,
		beacon:                     beacon,
		storageMap:                 storageMap,
		logger:                     zap.NewNop(),
		metrics:                    nopMetrics{},
	}

	for _, opt := range opts {
		opt(eh)
	}

	return eh, nil
}

func (eh *EventHandler) HandleBlockEventsStream(logs <-chan executionclient.BlockLogs, executeTasks bool) (lastProcessedBlock uint64, err error) {
	for blockLogs := range logs {
		logger := eh.logger.With(fields.BlockNumber(blockLogs.BlockNumber))

		start := time.Now()
		tasks, err := eh.processBlockEvents(blockLogs)
		logger.Debug("processed events from block",
			fields.Count(len(blockLogs.Logs)),
			fields.Took(time.Since(start)),
			zap.Error(err))

		if err != nil {
			return 0, fmt.Errorf("failed to process block events: %w", err)
		}

		lastProcessedBlock = blockLogs.BlockNumber
		if !executeTasks || len(tasks) == 0 {
			continue
		}

		logger.Debug("executing tasks", fields.Count(len(tasks)))

		for _, task := range tasks {
			logger = logger.With(fields.Type(task))
			logger.Debug("executing task")
			if err := task.Execute(); err != nil {
				// TODO: We log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				logger.Error("failed to execute task", zap.Error(err))
			} else {
				logger.Debug("executed task")
			}
		}
	}

	return
}

func (eh *EventHandler) processBlockEvents(block executionclient.BlockLogs) ([]Task, error) {
	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	lastProcessedBlock, found, err := eh.nodeStorage.GetLastProcessedBlock(txn)
	if err != nil {
		return nil, fmt.Errorf("get last processed block: %w", err)
	}
	if !found {
		lastProcessedBlock = new(big.Int).SetUint64(0)
	} else if lastProcessedBlock == nil {
		return nil, fmt.Errorf("last processed block is nil")
	}
	if lastProcessedBlock.Uint64() >= block.BlockNumber {
		// Same or higher block has already been processed, this should never happen!
		// Returning an error to signal that we should stop processing and
		// investigate the issue.
		//
		// TODO: this may happen during reorgs, and we should either
		// implement reorg support or only process finalized blocks.
		return nil, ErrInferiorBlock
	}

	var tasks []Task
	for _, log := range block.Logs {
		task, err := eh.processEvent(txn, log)
		if err != nil {
			return nil, err
		}
		if task != nil {
			tasks = append(tasks, task)
		}
	}

	if err := eh.nodeStorage.SaveLastProcessedBlock(txn, new(big.Int).SetUint64(block.BlockNumber)); err != nil {
		return nil, fmt.Errorf("set last processed block: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tasks, nil
}

func (eh *EventHandler) processEvent(txn basedb.Txn, event ethtypes.Log) (Task, error) {
	abiEvent, err := eh.eventParser.EventByID(event.Topics[0])
	if err != nil {
		eh.logger.Error("failed to find event by ID", zap.String("hash", event.Topics[0].String()))
		return nil, nil
	}

	switch abiEvent.Name {
	case OperatorAdded:
		operatorAddedEvent, err := eh.eventParser.ParseOperatorAdded(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		if err := eh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		eh.metrics.EventProcessed(abiEvent.Name)
		return nil, nil

	case OperatorRemoved:
		operatorRemovedEvent, err := eh.eventParser.ParseOperatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		if err := eh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		eh.metrics.EventProcessed(abiEvent.Name)
		return nil, nil

	case ValidatorAdded:
		validatorAddedEvent, err := eh.eventParser.ParseValidatorAdded(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		share, err := eh.handleValidatorAdded(txn, validatorAddedEvent)
		if err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		defer eh.metrics.EventProcessed(abiEvent.Name)

		if share == nil {
			return nil, nil
		}

		task := NewStartValidatorTask(eh.taskExecutor, share)

		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := eh.eventParser.ParseValidatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		validatorPubKey, err := eh.handleValidatorRemoved(txn, validatorRemovedEvent)
		if err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		defer eh.metrics.EventProcessed(abiEvent.Name)

		if validatorPubKey != nil {
			return NewStopValidatorTask(eh.taskExecutor, validatorPubKey), nil
		}

		return nil, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := eh.eventParser.ParseClusterLiquidated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		sharesToLiquidate, err := eh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		defer eh.metrics.EventProcessed(abiEvent.Name)

		if len(sharesToLiquidate) == 0 {
			return nil, nil
		}

		task := NewLiquidateClusterTask(eh.taskExecutor, clusterLiquidatedEvent.Owner, clusterLiquidatedEvent.OperatorIds, sharesToLiquidate)

		return task, nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := eh.eventParser.ParseClusterReactivated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		sharesToReactivate, err := eh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		defer eh.metrics.EventProcessed(abiEvent.Name)

		if len(sharesToReactivate) == 0 {
			return nil, nil
		}

		task := NewReactivateClusterTask(eh.taskExecutor, clusterReactivatedEvent.Owner, clusterReactivatedEvent.OperatorIds, sharesToReactivate)

		return task, nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := eh.eventParser.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			eh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		updated, err := eh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			eh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		defer eh.metrics.EventProcessed(abiEvent.Name)

		if !updated {
			return nil, nil
		}

		task := NewUpdateFeeRecipientTask(eh.taskExecutor, feeRecipientAddressUpdatedEvent.Owner, feeRecipientAddressUpdatedEvent.RecipientAddress)
		return task, nil

	default:
		eh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}

func (eh *EventHandler) HandleLocalEvents(localEvents []localevents.Event) error {
	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	for _, event := range localEvents {
		if err := eh.processLocalEvent(txn, event); err != nil {
			return fmt.Errorf("process local event: %w", err)
		}
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (eh *EventHandler) processLocalEvent(txn basedb.Txn, event localevents.Event) error {
	switch event.Name {
	case OperatorAdded:
		data := event.Data.(contract.ContractOperatorAdded)
		if err := eh.handleOperatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorAdded: %w", err)
		}
		return nil
	case OperatorRemoved:
		data := event.Data.(contract.ContractOperatorRemoved)
		if err := eh.handleOperatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorRemoved: %w", err)
		}
		return nil
	case ValidatorAdded:
		data := event.Data.(contract.ContractValidatorAdded)
		if _, err := eh.handleValidatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		return nil
	case ValidatorRemoved:
		data := event.Data.(contract.ContractValidatorRemoved)
		if _, err := eh.handleValidatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		return nil
	case ClusterLiquidated:
		data := event.Data.(contract.ContractClusterLiquidated)
		_, err := eh.handleClusterLiquidated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterLiquidated: %w", err)
		}
		return nil
	case ClusterReactivated:
		data := event.Data.(contract.ContractClusterReactivated)
		_, err := eh.handleClusterReactivated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterReactivated: %w", err)
		}
		return nil
	case FeeRecipientAddressUpdated:
		data := event.Data.(contract.ContractFeeRecipientAddressUpdated)
		_, err := eh.handleFeeRecipientAddressUpdated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}
		return nil
	default:
		eh.logger.Warn("unknown local event name", fields.Name(event.Name))
		return nil
	}
}
