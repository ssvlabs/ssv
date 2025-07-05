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
	ethtypes "github.com/ethereum/go-ethereum/core/types"
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
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
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

type DoppelgangerProvider interface {
	RemoveValidatorState(validatorIndex phase0.ValidatorIndex)
}

type EventHandler struct {
	nodeStorage         nodestorage.Storage
	eventParser         eventparser.Parser
	networkConfig       networkconfig.Network
	operatorDataStore   operatordatastore.OperatorDataStore
	operatorDecrypter   keys.OperatorDecrypter
	keyManager          ekm.KeyManager
	doppelgangerHandler DoppelgangerProvider
	validatorStore      registrystorage.ValidatorStore

	fullNode bool
	logger   *zap.Logger
}

func New(
	nodeStorage nodestorage.Storage,
	eventParser eventparser.Parser,
	networkConfig networkconfig.Network,
	operatorDataStore operatordatastore.OperatorDataStore,
	operatorDecrypter keys.OperatorDecrypter,
	keyManager ekm.KeyManager,
	doppelgangerHandler DoppelgangerProvider,
	validatorStore registrystorage.ValidatorStore,
	opts ...Option,
) (*EventHandler, error) {
	eh := &EventHandler{
		nodeStorage:         nodeStorage,
		eventParser:         eventParser,
		networkConfig:       networkConfig,
		operatorDataStore:   operatorDataStore,
		operatorDecrypter:   operatorDecrypter,
		keyManager:          keyManager,
		doppelgangerHandler: doppelgangerHandler,
		validatorStore:      validatorStore,
		logger:              zap.NewNop(),
	}

	for _, opt := range opts {
		opt(eh)
	}

	return eh, nil
}

func (eh *EventHandler) HandleBlockEventsStream(ctx context.Context, logs <-chan executionclient.BlockLogs) (lastProcessedBlock uint64, err error) {
	for blockLogs := range logs {
		logger := eh.logger.With(fields.BlockNumber(blockLogs.BlockNumber))

		start := time.Now()
		err := eh.processBlockEvents(ctx, blockLogs)
		logger.Debug("processed events from block",
			fields.Count(len(blockLogs.Logs)),
			fields.Took(time.Since(start)),
			zap.Error(err))

		if err != nil {
			return 0, fmt.Errorf("failed to process block events: %w", err)
		}
		lastProcessedBlock = blockLogs.BlockNumber

		observability.RecordUint64Value(ctx, lastProcessedBlock, lastProcessedBlockGauge.Record)
	}

	return
}

func (eh *EventHandler) processBlockEvents(ctx context.Context, block executionclient.BlockLogs) error {
	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	lastProcessedBlock, found, err := eh.nodeStorage.GetLastProcessedBlock(txn)
	if err != nil {
		return fmt.Errorf("get last processed block: %w", err)
	}
	if !found {
		lastProcessedBlock = new(big.Int).SetUint64(0)
	} else if lastProcessedBlock == nil {
		return fmt.Errorf("last processed block is nil")
	}
	if lastProcessedBlock.Uint64() >= block.BlockNumber {
		// Same or higher block has already been processed, this should never happen!
		// Returning an error to signal that we should stop processing and
		// investigate the issue.
		//
		// TODO: this may happen during reorgs, and we should either
		// implement reorg support or only process finalized blocks.
		return ErrInferiorBlock
	}

	for _, log := range block.Logs {
		if err := eh.processEvent(ctx, txn, log); err != nil {
			return err
		}
	}

	if err := eh.nodeStorage.SaveLastProcessedBlock(txn, new(big.Int).SetUint64(block.BlockNumber)); err != nil {
		return fmt.Errorf("set last processed block: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (eh *EventHandler) processEvent(ctx context.Context, txn basedb.Txn, event ethtypes.Log) error {
	abiEvent, err := eh.eventParser.EventByID(event.Topics[0])
	if err != nil {
		eh.logger.Error("failed to find event by ID", zap.String("hash", event.Topics[0].String()))
		return nil
	}

	switch abiEvent.Name {
	case OperatorAdded:
		operatorAddedEvent, err := eh.eventParser.ParseOperatorAdded(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		if err := eh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle OperatorAdded: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case OperatorRemoved:
		operatorRemovedEvent, err := eh.eventParser.ParseOperatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		if err := eh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case ValidatorAdded:
		validatorAddedEvent, err := eh.eventParser.ParseValidatorAdded(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleValidatorAdded(ctx, txn, validatorAddedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := eh.eventParser.ParseValidatorRemoved(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleValidatorRemoved(ctx, txn, validatorRemovedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle ValidatorRemoved: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case ClusterLiquidated:
		clusterLiquidatedEvent, err := eh.eventParser.ParseClusterLiquidated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case ClusterReactivated:
		clusterReactivatedEvent, err := eh.eventParser.ParseClusterReactivated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case FeeRecipientAddressUpdated:
		feeRecipientAddressUpdatedEvent, err := eh.eventParser.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	case ValidatorExited:
		validatorExitedEvent, err := eh.eventParser.ParseValidatorExited(event)
		if err != nil {
			eh.logger.Warn("could not parse event",
				fields.EventName(abiEvent.Name),
				zap.Error(err))
			recordEventProcessFailure(ctx, abiEvent.Name)
			return nil
		}

		_, err = eh.handleValidatorExited(txn, validatorExitedEvent)
		if err != nil {
			recordEventProcessFailure(ctx, abiEvent.Name)
			var malformedEventError *MalformedEventError
			if errors.As(err, &malformedEventError) {
				return nil
			}
			return fmt.Errorf("handle ValidatorExited: %w", err)
		}

		eventsProcessSuccessCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(abiEvent.Name)))
		return nil

	default:
		eh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil
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
