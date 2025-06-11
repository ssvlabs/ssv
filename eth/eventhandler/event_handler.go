// Package eventhandler provides support for handling registry contract events
// and persisting them to the database.
package eventhandler

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/localevents"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
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
	ValidatorExited            = "ValidatorExited"
)

var (
	// ErrInferiorBlock is returned when trying to process a block that is
	// not higher than the last processed block.
	ErrInferiorBlock = errors.New("block is not higher than the last processed block")
)

type taskExecutor interface {
	StopValidator(pubKey spectypes.ValidatorPK) error
	LiquidateCluster(owner ethcommon.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner ethcommon.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient ethcommon.Address) error
	ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex, ownValidator bool) error
}

type DoppelgangerProvider interface {
	RemoveValidatorState(validatorIndex phase0.ValidatorIndex)
}

type EventHandler struct {
	nodeStorage         nodestorage.Storage
	taskExecutor        taskExecutor
	eventParser         eventparser.Parser
	networkConfig       networkconfig.NetworkConfig
	operatorDataStore   operatordatastore.OperatorDataStore
	operatorDecrypter   keys.OperatorDecrypter
	keyManager          ekm.KeyManager
	doppelgangerHandler DoppelgangerProvider

	fullNode bool
	logger   *zap.Logger
}

func New(
	nodeStorage nodestorage.Storage,
	eventParser eventparser.Parser,
	taskExecutor taskExecutor,
	networkConfig networkconfig.NetworkConfig,
	operatorDataStore operatordatastore.OperatorDataStore,
	operatorDecrypter keys.OperatorDecrypter,
	keyManager ekm.KeyManager,
	doppelgangerHandler DoppelgangerProvider,
	opts ...Option,
) (*EventHandler, error) {
	eh := &EventHandler{
		nodeStorage:         nodeStorage,
		taskExecutor:        taskExecutor,
		eventParser:         eventParser,
		networkConfig:       networkConfig,
		operatorDataStore:   operatorDataStore,
		operatorDecrypter:   operatorDecrypter,
		keyManager:          keyManager,
		doppelgangerHandler: doppelgangerHandler,
		logger:              zap.NewNop(),
	}

	for _, opt := range opts {
		opt(eh)
	}

	return eh, nil
}

func (eh *EventHandler) HandleBlockEventsStream(ctx context.Context, logs <-chan executionclient.BlockLogs, executeTasks bool) (lastProcessedBlock uint64, err error) {
	for blockLogs := range logs {
		logger := eh.logger.With(fields.BlockNumber(blockLogs.BlockNumber))

		start := time.Now()
		tasks, err := eh.processBlockEvents(ctx, blockLogs)
		logger.Debug("processed events from block",
			fields.Count(len(blockLogs.Logs)),
			fields.Took(time.Since(start)),
			zap.Error(err))

		if err != nil {
			return 0, fmt.Errorf("failed to process block events: %w", err)
		}
		lastProcessedBlock = blockLogs.BlockNumber

		observability.RecordUint64Value(ctx, lastProcessedBlock, lastProcessedBlockGauge.Record)

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

func (eh *EventHandler) processBlockEvents(ctx context.Context, block executionclient.BlockLogs) ([]Task, error) {
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
		task, err := eh.processEvent(ctx, txn, log)
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

func (eh *EventHandler) processEvent(ctx context.Context, txn basedb.Txn, event ethtypes.Log) (Task, error) {
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
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		if err := eh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		return nil, nil

	case OperatorRemoved:
		operatorRemovedEvent, err := eh.eventParser.ParseOperatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		if err := eh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		return nil, nil

	case ValidatorAdded:
		validatorAddedEvent, err := eh.eventParser.ParseValidatorAdded(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		share, err := eh.handleValidatorAdded(ctx, txn, validatorAddedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		if share == nil {
			return nil, nil
		}

		return nil, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := eh.eventParser.ParseValidatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		validatorPubKey, err := eh.handleValidatorRemoved(ctx, txn, validatorRemovedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		if validatorPubKey != emptyPK {
			return NewStopValidatorTask(eh.taskExecutor, validatorPubKey), nil
		}

		return nil, nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := eh.eventParser.ParseClusterLiquidated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		sharesToLiquidate, err := eh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

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

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		sharesToReactivate, err := eh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

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

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		updated, err := eh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		if !updated {
			return nil, nil
		}

		task := NewUpdateFeeRecipientTask(eh.taskExecutor, feeRecipientAddressUpdatedEvent.Owner, feeRecipientAddressUpdatedEvent.RecipientAddress)
		return task, nil

	case ValidatorExited:
		validatorExitedEvent, err := eh.eventParser.ParseValidatorExited(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))

			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil, nil
		}

		exitDescriptor, err := eh.handleValidatorExited(txn, validatorExitedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)

			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil, nil
			}
			return nil, fmt.Errorf("handle ValidatorExited: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))

		if exitDescriptor == nil {
			return nil, nil
		}

		task := NewExitValidatorTask(
			eh.taskExecutor,
			exitDescriptor.PubKey,
			exitDescriptor.BlockNumber,
			exitDescriptor.ValidatorIndex,
			exitDescriptor.OwnValidator,
		)
		return task, nil

	default:
		eh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}

func (eh *EventHandler) HandleLocalEvents(ctx context.Context, localEvents []localevents.Event) error {
	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	for _, event := range localEvents {
		if err := eh.processLocalEvent(ctx, txn, event); err != nil {
			return fmt.Errorf("process local event: %w", err)
		}
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (eh *EventHandler) processLocalEvent(ctx context.Context, txn basedb.Txn, event localevents.Event) error {
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
		if _, err := eh.handleValidatorAdded(ctx, txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		return nil
	case ValidatorRemoved:
		data := event.Data.(contract.ContractValidatorRemoved)
		if _, err := eh.handleValidatorRemoved(ctx, txn, &data); err != nil {
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
	case ValidatorExited:
		data := event.Data.(contract.ContractValidatorExited)
		_, err := eh.handleValidatorExited(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ValidatorExited: %w", err)
		}
		return nil
	default:
		eh.logger.Warn("unknown local event name", fields.Name(event.Name))
		return nil
	}
}
