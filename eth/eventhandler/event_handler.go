// Package eventhandler provides support for handling registry contract events
// and persisting them to the database.
package eventhandler

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/eth/localevents"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	nodestorage "github.com/bloxapp/ssv/operator/storage"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	// ErrInferiorBlock is returned when trying to process a block that is
	// not higher than the last processed block.
	ErrInferiorBlock = errors.New("block is not higher than the last processed block")
)

type TaskExecutor interface {
	StartValidator(share *ssvtypes.SSVShare) error
	StopValidator(pubKey spectypes.ValidatorPK) error
	LiquidateCluster(owner ethcommon.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner ethcommon.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient ethcommon.Address) error
	ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex) error
}

type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

type OperatorData interface {
	GetOperatorData() *storage.OperatorData
	SetOperatorData(*storage.OperatorData)
}

// EventTrace records the outcome of an event going through EventHandler.
type EventTrace struct {
	Log *ethtypes.Log

	// Event is the parsed SSV event, or nil if the event was not parsed.
	Event interface{}

	// Error is the error that occurred while parsing or handling the event.
	// A nil errors means that the event is valid and was handled successfully.
	Error error
}

type EventHandler struct {
	nodeStorage                nodestorage.Storage
	taskExecutor               TaskExecutor
	eventParser                eventparser.Parser
	networkConfig              networkconfig.NetworkConfig
	operatorData               OperatorData
	shareEncryptionKeyProvider ShareEncryptionKeyProvider
	keyManager                 spectypes.KeyManager
	storageMap                 *qbftstorage.QBFTStores

	fullNode bool
	logger   *zap.Logger
	metrics  metrics
}

func New(
	nodeStorage nodestorage.Storage,
	eventParser eventparser.Parser,
	taskExecutor TaskExecutor,
	networkConfig networkconfig.NetworkConfig,
	operatorData OperatorData,
	shareEncryptionKeyProvider ShareEncryptionKeyProvider,
	keyManager spectypes.KeyManager,
	storageMap *qbftstorage.QBFTStores,
	opts ...Option,
) (*EventHandler, error) {
	eh := &EventHandler{
		nodeStorage:                nodeStorage,
		taskExecutor:               taskExecutor,
		eventParser:                eventParser,
		networkConfig:              networkConfig,
		operatorData:               operatorData,
		shareEncryptionKeyProvider: shareEncryptionKeyProvider,
		keyManager:                 keyManager,
		storageMap:                 storageMap,
		logger:                     zap.NewNop(),
		metrics:                    nopMetrics{},
	}

	for _, opt := range opts {
		opt(eh)
	}

	return eh, nil
}

func (eh *EventHandler) HandleBlockEventsStream(
	logs <-chan executionclient.BlockLogs,
	executeTasks bool,
	eventTraces chan<- EventTrace,
) (lastProcessedBlock uint64, err error) {
	for blockLogs := range logs {
		logger := eh.logger.With(fields.BlockNumber(blockLogs.BlockNumber))

		start := time.Now()
		tasks, err := eh.processBlockEvents(blockLogs, eventTraces)
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

func (eh *EventHandler) processBlockEvents(block executionclient.BlockLogs, eventTraces chan<- EventTrace) ([]Task, error) {
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
		task, err := eh.processEvent(txn, log, eventTraces)
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

func (eh *EventHandler) processEvent(txn basedb.Txn, event ethtypes.Log, eventTraces chan<- EventTrace) (Task, error) {
	eventTrace := EventTrace{Log: &event}
	if eventTraces != nil {
		defer func() {
			eventTraces <- eventTrace
		}()
	}

	abiEvent, err := eh.eventParser.EventByID(event.Topics[0])
	if err != nil {
		eventTrace.Error = fmt.Errorf("failed to find event by ID: %w", err)
		eh.logger.Warn("failed to find event by ID", zap.String("hash", event.Topics[0].String()))
		return nil, nil
	}

	parsedEvent, err := eh.eventParser.ParseEvent(abiEvent, event)
	if err != nil {
		eventTrace.Error = fmt.Errorf("failed to parse event: %w", err)
		eh.logger.Warn("could not parse event",
			fields.EventName(abiEvent.Name),
			zap.Error(err))
		eh.metrics.EventProcessingFailed(abiEvent.Name)
		return nil, nil
	}
	eventTrace.Event = parsedEvent

	task, err := eh.handleEvent(txn, abiEvent, parsedEvent)
	if err != nil {
		eventTrace.Error = fmt.Errorf("failed to handle event: %w", err)
		eh.metrics.EventProcessingFailed(abiEvent.Name)
		var malformedEventError *MalformedEventError
		if errors.As(err, &malformedEventError) {
			return nil, nil
		}
		return nil, err
	}

	eh.metrics.EventProcessed(abiEvent.Name)
	return task, nil
}

func (eh *EventHandler) handleEvent(txn basedb.Txn, abiEvent *abi.Event, parsedEvent interface{}) (Task, error) {
	switch abiEvent.Name {
	case eventparser.OperatorAdded:
		err := eh.handleOperatorAdded(txn, parsedEvent.(*contract.ContractOperatorAdded))
		return nil, err
	case eventparser.OperatorRemoved:
		err := eh.handleOperatorRemoved(txn, parsedEvent.(*contract.ContractOperatorRemoved))
		return nil, err
	case eventparser.ValidatorAdded:
		share, err := eh.handleValidatorAdded(txn, parsedEvent.(*contract.ContractValidatorAdded))
		if err != nil || share == nil {
			return nil, err
		}
		return NewStartValidatorTask(eh.taskExecutor, share), nil
	case eventparser.ValidatorRemoved:
		validatorPubKey, err := eh.handleValidatorRemoved(txn, parsedEvent.(*contract.ContractValidatorRemoved))
		if err != nil || validatorPubKey == nil {
			return nil, err
		}
		return NewStopValidatorTask(eh.taskExecutor, validatorPubKey), nil
	case eventparser.ClusterLiquidated:
		sharesToLiquidate, err := eh.handleClusterLiquidated(txn, parsedEvent.(*contract.ContractClusterLiquidated))
		if err != nil || len(sharesToLiquidate) == 0 {
			return nil, err
		}
		return NewLiquidateClusterTask(eh.taskExecutor, parsedEvent.(*contract.ContractClusterLiquidated).Owner, parsedEvent.(*contract.ContractClusterLiquidated).OperatorIds, sharesToLiquidate), nil
	case eventparser.ClusterReactivated:
		sharesToReactivate, err := eh.handleClusterReactivated(txn, parsedEvent.(*contract.ContractClusterReactivated))
		if err != nil || len(sharesToReactivate) == 0 {
			return nil, err
		}
		return NewReactivateClusterTask(eh.taskExecutor, parsedEvent.(*contract.ContractClusterReactivated).Owner, parsedEvent.(*contract.ContractClusterReactivated).OperatorIds, sharesToReactivate), nil
	case eventparser.FeeRecipientAddressUpdated:
		updated, err := eh.handleFeeRecipientAddressUpdated(txn, parsedEvent.(*contract.ContractFeeRecipientAddressUpdated))
		if err != nil || !updated {
			return nil, err
		}
		return NewUpdateFeeRecipientTask(eh.taskExecutor, parsedEvent.(*contract.ContractFeeRecipientAddressUpdated).Owner, parsedEvent.(*contract.ContractFeeRecipientAddressUpdated).RecipientAddress), nil
	case ValidatorExited:
		exitDescriptor, err := eh.handleValidatorExited(txn, parsedEvent.(*contract.ContractValidatorExited))
		if err != nil || exitDescriptor == nil {
			return nil, err
		}
		return NewExitValidatorTask(eh.taskExecutor, exitDescriptor.PubKey, exitDescriptor.BlockNumber, exitDescriptor.ValidatorIndex), nil

	default:
		return nil, fmt.Errorf("unknown event name: %s", abiEvent.Name)
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
	case eventparser.OperatorAdded:
		data := event.Data.(contract.ContractOperatorAdded)
		if err := eh.handleOperatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorAdded: %w", err)
		}
		return nil
	case eventparser.OperatorRemoved:
		data := event.Data.(contract.ContractOperatorRemoved)
		if err := eh.handleOperatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle OperatorRemoved: %w", err)
		}
		return nil
	case eventparser.ValidatorAdded:
		data := event.Data.(contract.ContractValidatorAdded)
		if _, err := eh.handleValidatorAdded(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		return nil
	case eventparser.ValidatorRemoved:
		data := event.Data.(contract.ContractValidatorRemoved)
		if _, err := eh.handleValidatorRemoved(txn, &data); err != nil {
			return fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		return nil
	case eventparser.ClusterLiquidated:
		data := event.Data.(contract.ContractClusterLiquidated)
		_, err := eh.handleClusterLiquidated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterLiquidated: %w", err)
		}
		return nil
	case eventparser.ClusterReactivated:
		data := event.Data.(contract.ContractClusterReactivated)
		_, err := eh.handleClusterReactivated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle ClusterReactivated: %w", err)
		}
		return nil
	case eventparser.FeeRecipientAddressUpdated:
		data := event.Data.(contract.ContractFeeRecipientAddressUpdated)
		_, err := eh.handleFeeRecipientAddressUpdated(txn, &data)
		if err != nil {
			return fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}
		return nil
	case eventparser.ValidatorExited:
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
