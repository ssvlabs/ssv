package eventdatahandler

import (
	"crypto/rsa"
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

		logger = logger.With(fields.Count(len(tasks)))
		logger.Info("executing tasks")

		cleanTaskList := cleanTaskList(tasks)
		for _, task := range cleanTaskList {
			edh.logger.With(
				zap.String("event_type", task.GetEventType()),
			)
			edh.logger.Debug("going to execute remote task")
			if err := task.Execute(); err != nil {
				// TODO: We log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				edh.logger.Error("failed to execute remote task")
			} else {
				edh.logger.Debug("executed remote task")
			}
		}
		logger.Info("task execution finished")
	}

	return lastProcessedBlock, nil
}

func (edh *EventDataHandler) processBlockEvents(blockEvents eventbatcher.BlockEvents) ([]*Task, error) {
	txn := edh.eventDB.RWTxn()
	defer txn.Discard()

	var tasks []*Task
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

func (edh *EventDataHandler) processEvent(txn eventdb.RW, event ethtypes.Log) (*Task, error) {
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
		task := NewTask(edh, event, nil)
		return task, nil

	case ValidatorRemoved:
		validatorRemovedEvent, err := edh.filterer.ParseValidatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorRemoved: %w", err)
		}

		if err := edh.handleValidatorRemoved(txn, validatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		task := NewTask(edh, event, nil)

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

		task := NewTask(edh, event, sharesToLiquidate)

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

		task := NewTask(edh, event, sharesToEnable)

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

		if !updated {
			return nil, fmt.Errorf("provided recipient address is the same")
		}

		task := NewTask(edh, event, nil)

		return task, nil

	default:
		edh.logger.Warn("unknown event name", fields.Name(abiEvent.Name))
		return nil, nil
	}
}

// find and remove opposite tasks (start-stop, stop-start, liquidate-reactivate, reactivate-liquidate)
// find superseding tasks and remove superseded ones (updateFee-updateFee)
func cleanTaskList(tasks []*Task) []*Task {
	taskMap := make(map[*Task]bool)
	var resTask []*Task
	for _, task := range tasks {
		if _, exist := taskMap[task]; !exist {
			taskMap[task] = true
		}
	}
	for i, task := range tasks {
		for j := i + 1; j < len(tasks); j++ {
			if task.GetEventType() == ValidatorAdded && tasks[j].GetEventType() == ValidatorRemoved || task.GetEventType() == ValidatorRemoved && tasks[j].GetEventType() == ValidatorAdded {
				delete(taskMap, tasks[j])
				delete(taskMap, task)
			}
			if task.GetEventType() == ClusterLiquidated && tasks[j].GetEventType() == ClusterReactivated || task.GetEventType() == ClusterReactivated && tasks[j].GetEventType() == ClusterLiquidated {
				delete(taskMap, tasks[j])
				delete(taskMap, task)
			}
			if task.GetEventType() == FeeRecipientAddressUpdated && tasks[j].GetEventType() == FeeRecipientAddressUpdated {
				delete(taskMap, task)
			}
		}
	}
	for task := range taskMap {
		resTask = append(resTask, task)
	}
	return resTask
}

func (edh *EventDataHandler) HandleLocalEventsStream(localEventsCh <-chan []localevents.Event, executeTasks bool) error {

	for localevents := range localEventsCh {

		tasks, err := edh.processLocalEvents(localevents)
		if err != nil {
			return fmt.Errorf("process local events: %w", err)
		}

		if !executeTasks || len(tasks) == 0 {
			continue
		}

		cleanTaskList := cleanTaskList(tasks)
		for _, task := range cleanTaskList {
			edh.logger.With(zap.String("event_type", task.GetEventType()))
			edh.logger.Debug("going to execute local task")
			if err := task.Execute(); err != nil {
				// TODO: We log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				edh.logger.Error("failed to execute local task")
			} else {
				edh.logger.Debug("executed local task")
			}
		}

		edh.logger.Info("task execution finished")
	}

	return nil
}

func (edh *EventDataHandler) processLocalEvent(txn eventdb.RW, event localevents.Event) (*Task, error) {
	switch event.Name {
	case OperatorAdded:
		e := event.Data.(contract.ContractOperatorAdded)
		if err := edh.handleOperatorAdded(txn, &e); err != nil {
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}
		return nil, nil
	case OperatorRemoved:
		e := event.Data.(contract.ContractOperatorRemoved)
		if err := edh.handleOperatorRemoved(txn, &e); err != nil {
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}
		return nil, nil
	case ValidatorAdded:
		e := event.Data.(contract.ContractValidatorAdded)
		if err := edh.handleValidatorAdded(txn, &e); err != nil {
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		task := NewTask(edh, &event, nil)
		return task, nil
	case ValidatorRemoved:
		e := event.Data.(contract.ContractValidatorRemoved)
		if err := edh.handleValidatorRemoved(txn, &e); err != nil {
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		task := NewTask(edh, &event, nil)
		return task, nil
	case ClusterLiquidated:
		e := event.Data.(contract.ContractClusterLiquidated)
		sharesToLiquidate, err := edh.handleClusterLiquidated(txn, &e)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}
		task := NewTask(edh, &event, sharesToLiquidate)
		return task, nil
	case ClusterReactivated:
		e := event.Data.(contract.ContractClusterReactivated)
		sharesToEnable, err := edh.handleClusterReactivated(txn, &e)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}
		task := NewTask(edh, &event, sharesToEnable)
		return task, nil
	case FeeRecipientAddressUpdated:
		e := event.Data.(contract.ContractFeeRecipientAddressUpdated)
		updated, err := edh.handleFeeRecipientAddressUpdated(txn, &e)
		if err != nil {
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}
		if !updated {
			return nil, fmt.Errorf("provided recipient address is the same")
		}
		task := NewTask(edh, &event, nil)
		return task, nil
	default:
		edh.logger.Warn("unknown event name", fields.Name(event.Name))
		return nil, nil
	}
}

func (edh *EventDataHandler) processLocalEvents(localEvents []localevents.Event) ([]*Task, error) {
	txn := edh.eventDB.RWTxn()
	defer txn.Discard()

	var tasks []*Task
	for _, event := range localEvents {
		task, err := edh.processLocalEvent(txn, event)
		if err != nil {
			return nil, err
		}

		if task != nil {
			tasks = append(tasks, task)
		}
	}

	if err := txn.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tasks, nil
}
