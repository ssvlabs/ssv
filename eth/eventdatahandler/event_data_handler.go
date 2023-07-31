package eventdatahandler

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

type taskExecutor interface {
	StartValidator(share *ssvtypes.SSVShare) error
	StopValidator(publicKey []byte) error
	LiquidateCluster(owner ethcommon.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner ethcommon.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient ethcommon.Address) error
}

type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

type OperatorData interface {
	GetOperatorData() *storage.OperatorData
	SetOperatorData(*storage.OperatorData)
}

type EventDataHandler struct {
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
) (*EventDataHandler, error) {
	edh := &EventDataHandler{
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
		opt(edh)
	}

	return edh, nil
}

func (edh *EventDataHandler) HandleBlockEventsStream(logs <-chan executionclient.BlockLogs, executeTasks bool) (lastProcessedBlock uint64, err error) {
	for blockLogs := range logs {
		logger := edh.logger.With(fields.BlockNumber(blockLogs.BlockNumber))

		start := time.Now()
		tasks, err := edh.processBlockEvents(blockLogs)
		logger.Debug("processed events from block",
			fields.Count(len(blockLogs.Logs)),
			fields.Took(time.Since(start)),
			zap.Error(err))

		if err != nil {
			return 0, fmt.Errorf("process block events: %w", err)
		}

		lastProcessedBlock = blockLogs.BlockNumber
		if !executeTasks || len(tasks) == 0 {
			continue
		}

		logger.Debug("executing tasks", fields.Count(len(tasks)))

		for _, task := range tasks {
			logger = logger.With(fields.Type(task))
			logger.Debug("going to execute task")
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

func (edh *EventDataHandler) processBlockEvents(block executionclient.BlockLogs) ([]Task, error) {
	txn := edh.nodeStorage.Begin()
	defer txn.Discard()

	var tasks []Task
	for _, log := range block.Logs {
		task, err := edh.processEvent(txn, log)
		if err != nil {
			return nil, err
		}

		if task != nil {
			tasks = append(tasks, task)
		}
	}

	if err := edh.nodeStorage.SaveLastProcessedBlock(txn, new(big.Int).SetUint64(block.BlockNumber)); err != nil {
		return nil, fmt.Errorf("set last processed block: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tasks, nil
}

func (edh *EventDataHandler) processEvent(txn basedb.Txn, event ethtypes.Log) (Task, error) {
	abiEvent, err := edh.eventParser.EventByID(event.Topics[0])
	if err != nil {
		edh.logger.Error("failed to find event by ID", zap.String("hash", event.Topics[0].String()))
		return nil, nil
	}

	switch abiEvent.Name {
	case OperatorAdded:
		operatorAddedEvent, err := edh.eventParser.ParseOperatorAdded(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
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
		operatorRemovedEvent, err := edh.eventParser.ParseOperatorRemoved(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
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
		validatorAddedEvent, err := edh.eventParser.ParseValidatorAdded(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		share, err := edh.handleValidatorAdded(txn, validatorAddedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		defer edh.metrics.EventProcessed(abiEvent.Name)

		if share == nil {
			return nil, nil
		}

		task := NewStartValidatorTask(edh.taskExecutor, share)

		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := edh.eventParser.ParseValidatorRemoved(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		sharePK, err := edh.handleValidatorRemoved(txn, validatorRemovedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		defer edh.metrics.EventProcessed(abiEvent.Name)

		if sharePK == nil {
			return nil, nil
		}

		task := NewStopValidatorTask(edh.taskExecutor, validatorRemovedEvent.PublicKey)

		return task, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := edh.eventParser.ParseClusterLiquidated(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
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

		defer edh.metrics.EventProcessed(abiEvent.Name)

		if len(sharesToLiquidate) == 0 {
			return nil, nil
		}

		task := NewLiquidateClusterTask(edh.taskExecutor, clusterLiquidatedEvent.Owner, clusterLiquidatedEvent.OperatorIds, sharesToLiquidate)

		return task, nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := edh.eventParser.ParseClusterReactivated(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
		}

		sharesToReactivate, err := edh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			edh.metrics.EventProcessingFailed(abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		defer edh.metrics.EventProcessed(abiEvent.Name)

		if len(sharesToReactivate) == 0 {
			return nil, nil
		}

		task := NewReactivateClusterTask(edh.taskExecutor, clusterReactivatedEvent.Owner, clusterReactivatedEvent.OperatorIds, sharesToReactivate)

		return task, nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := edh.eventParser.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			edh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			edh.metrics.EventProcessingFailed(abiEvent.Name)
			return nil, nil
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

		defer edh.metrics.EventProcessed(abiEvent.Name)

		if !updated {
			return nil, nil
		}

		task := NewUpdateFeeRecipientTask(edh.taskExecutor, feeRecipientAddressUpdatedEvent.Owner, feeRecipientAddressUpdatedEvent.RecipientAddress)
		return task, nil

	default:
		edh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}

func (edh *EventDataHandler) HandleLocalEvents(localEvents []localevents.Event) error {
	txn := edh.nodeStorage.Begin()
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

func (edh *EventDataHandler) processLocalEvent(txn basedb.Txn, event localevents.Event) error {
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
		if _, err := edh.handleValidatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		return nil
	case ValidatorRemoved:
		data := event.Data.(contract.ContractValidatorRemoved)
		if _, err := edh.handleValidatorRemoved(txn, &data); err != nil {
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
