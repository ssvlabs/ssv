package eventdatahandler

import (
	"crypto/rsa"
	"fmt"

	"github.com/bloxapp/ssv-spec/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/eth/eventdb"
	"github.com/bloxapp/ssv/eth/executionclient"
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
		return nil, fmt.Errorf("error handling contract ABI %s", err.Error())
	}

	filterer, err := client.Filterer()
	if err != nil {
		return nil, fmt.Errorf("error binding contract filterer %s", err.Error())
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

func (edh *EventDataHandler) HandleBlockEventsStream(blockEventsCh <-chan eventbatcher.BlockEvents) error {
	for blockEvents := range blockEventsCh {
		logger := edh.logger.With(fields.BlockNumber(blockEvents.BlockNumber))

		logger.Info("processing block events")
		tasks, err := edh.processBlockEvents(blockEvents)
		if err != nil {
			return fmt.Errorf("process block events: %w", err)
		}

		logger = logger.With(fields.Count(len(tasks)))
		logger.Info("processed block events")

		if len(tasks) == 0 {
			continue
		}

		logger.Info("executing tasks")

		// TODO:
		// 1) find and remove opposite tasks (start-stop, stop-start, liquidate-reactivate, reactivate-liquidate)
		// 2) find superseding tasks and remove superseded ones (updateFee-updateFee)
		cleanTaskList := cleanTaskList(tasks)
		for _, task := range cleanTaskList {
			if err := task.Execute(); err != nil {
				// TODO: Log failed task until we discuss how we want to handle this case. We likely need to crash the node in this case.
				return fmt.Errorf("execute task: %w", err)
			}
		}

		logger.Info("executed tasks")
	}

	return nil
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

	if err := txn.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return tasks, nil
}

func (edh *EventDataHandler) processEvent(txn eventdb.RW, event ethtypes.Log) (*Task, error) {
	abiEvent, err := edh.abi.EventByID(event.Topics[0])
	if err != nil {
		return nil, err
	}

	switch abiEvent.Name {
	case EventType.String(EventType(0)):
		operatorAddedEvent, err := edh.filterer.ParseOperatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorAdded: %w", err)
		}

		if err := edh.handleOperatorAdded(txn, operatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorAdded: %w", err)
		}

		return nil, nil

	case EventType.String(EventType(1)):
		operatorRemovedEvent, err := edh.filterer.ParseOperatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse OperatorRemoved: %w", err)
		}

		if err := edh.handleOperatorRemoved(txn, operatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle OperatorRemoved: %w", err)
		}

		return nil, nil

	case EventType.String(EventType(2)):
		validatorAddedEvent, err := edh.filterer.ParseValidatorAdded(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorAdded: %w", err)
		}

		if err := edh.handleValidatorAdded(txn, validatorAddedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorAdded: %w", err)
		}
		task := NewTask(EventType(2), edh, event, nil)
		return task, nil

	case EventType.String(EventType(3)):
		validatorRemovedEvent, err := edh.filterer.ParseValidatorRemoved(event)
		if err != nil {
			return nil, fmt.Errorf("parse ValidatorRemoved: %w", err)
		}

		if err := edh.handleValidatorRemoved(txn, validatorRemovedEvent); err != nil {
			return nil, fmt.Errorf("handle ValidatorRemoved: %w", err)
		}
		task := NewTask(EventType(3), edh, event, nil)

		return task, nil

	case EventType.String(EventType(4)):
		clusterLiquidatedEvent, err := edh.filterer.ParseClusterLiquidated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterLiquidated: %w", err)
		}

		sharesToLiquidate, err := edh.handleClusterLiquidated(txn, clusterLiquidatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterLiquidated: %w", err)
		}

		task := NewTask(EventType(4), edh, event, sharesToLiquidate)

		return task, nil

	case EventType.String(EventType(5)):
		clusterReactivatedEvent, err := edh.filterer.ParseClusterReactivated(event)
		if err != nil {
			return nil, fmt.Errorf("parse ClusterReactivated: %w", err)
		}

		sharesToEnable, err := edh.handleClusterReactivated(txn, clusterReactivatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle ClusterReactivated: %w", err)
		}

		task := NewTask(EventType(5), edh, event, sharesToEnable)

		return task, nil

	case EventType.String(EventType(6)):
		feeRecipientAddressUpdatedEvent, err := edh.filterer.ParseFeeRecipientAddressUpdated(event)
		if err != nil {
			return nil, fmt.Errorf("parse FeeRecipientAddressUpdated: %w", err)
		}

		updated, err := edh.handleFeeRecipientAddressUpdated(txn, feeRecipientAddressUpdatedEvent)
		if err != nil {
			return nil, fmt.Errorf("handle FeeRecipientAddressUpdated: %w", err)
		}

		if !updated {
			return &Task{}, fmt.Errorf("Provided receipeint address is the same")
		}

		task := NewTask(EventType(6), edh, event, nil)

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
			if task.EventType == EventType(2) && tasks[j].EventType == EventType(3) || task.EventType == EventType(3) && tasks[j].EventType == EventType(2) {
				fmt.Printf("Task event to delete %s \n", tasks[j].EventType.String())
				delete(taskMap, tasks[j])
				delete(taskMap, task)
			}
			if task.EventType == EventType(4) && tasks[j].EventType == EventType(5) || task.EventType == EventType(5) && tasks[j].EventType == EventType(4) {
				delete(taskMap, tasks[j])
				delete(taskMap, task)
			}
			if task.EventType == EventType(6) && tasks[j].EventType == EventType(6) {
				delete(taskMap, task)
			}
		}
	}
	for task, _ := range taskMap {
		resTask = append(resTask, task)
	}
	return resTask
}
