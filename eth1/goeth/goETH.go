package goeth

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/utils/tasks"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"
)

const (
	healthCheckTimeout        = 500 * time.Millisecond
	blocksInBatch      uint64 = 100000
)

// ClientOptions are the options for the client
type ClientOptions struct {
	Ctx                  context.Context
	NodeAddr             string
	RegistryContractAddr string
	ContractABI          string
	ConnectionTimeout    time.Duration

	AbiVersion eth1.Version
}

// eth1Client is the internal implementation of Client
type eth1Client struct {
	ctx  context.Context
	conn *ethclient.Client

	nodeAddr             string
	registryContractAddr string
	contractABI          string
	connectionTimeout    time.Duration

	eventsFeed *event.Feed

	abiVersion eth1.Version
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &eth1Client{}

// NewEth1Client creates a new instance
func NewEth1Client(logger *zap.Logger, opts ClientOptions) (eth1.Client, error) {
	ec := eth1Client{
		ctx:                  opts.Ctx,
		nodeAddr:             opts.NodeAddr,
		registryContractAddr: opts.RegistryContractAddr,
		contractABI:          opts.ContractABI,
		connectionTimeout:    opts.ConnectionTimeout,
		eventsFeed:           new(event.Feed),
		abiVersion:           opts.AbiVersion,
	}

	if err := ec.connect(logger); err != nil {
		logger.Error("failed to connect to the execution client", zap.Error(err))
		return nil, err
	}

	return &ec, nil
}

// EventsFeed returns the contract events feed
func (ec *eth1Client) EventsFeed() *event.Feed {
	return ec.eventsFeed
}

// Start streams events from the contract
func (ec *eth1Client) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameEthClient)
	err := ec.streamSmartContractEvents(logger)
	if err != nil {
		logger.Error("Failed to init operator contract address subject", zap.Error(err))
	}
	return err
}

// Sync reads events history
func (ec *eth1Client) Sync(logger *zap.Logger, fromBlock *big.Int) error {
	err := ec.syncSmartContractsEvents(logger, fromBlock)
	if err != nil {
		logger.Error("Failed to sync contract events", zap.Error(err))
	}
	return err
}

// IsReady returns if eth1 is currently ready: responds to requests and not in the syncing state.
func (ec *eth1Client) IsReady(ctx context.Context) (bool, error) {
	sp, err := ec.conn.SyncProgress(ctx)
	if err != nil {
		reportNodeStatus(statusUnknown)
		return false, err
	}

	if sp != nil {
		reportNodeStatus(statusSyncing)
		return false, nil
	}

	reportNodeStatus(statusOK)
	return true, nil
}

// HealthCheck provides health status of eth1 node
func (ec *eth1Client) HealthCheck() []string {
	if ec.conn == nil {
		return []string{"not connected to eth1 node"}
	}
	ctx, cancel := context.WithTimeout(ec.ctx, healthCheckTimeout)
	defer cancel()
	sp, err := ec.conn.SyncProgress(ctx)
	if err != nil {
		reportNodeStatus(statusUnknown)
		return []string{"could not get eth1 node sync progress"}
	}
	if sp != nil {
		reportNodeStatus(statusSyncing)
		return []string{fmt.Sprintf("eth1 node is currently syncing: starting=%d, current=%d, highest=%d",
			sp.StartingBlock, sp.CurrentBlock, sp.HighestBlock)}
	}
	// eth1 node is connected and synced
	reportNodeStatus(statusOK)

	return []string{}
}

// connect connects to eth1 client
func (ec *eth1Client) connect(logger *zap.Logger) error {
	// Create an IPC based RPC connection to a remote node
	logger.Info("execution client: connecting", fields.Address(ec.nodeAddr))
	ctx, cancel := context.WithTimeout(context.Background(), ec.connectionTimeout)
	defer cancel()
	conn, err := ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		logger.Error("execution client: can't connect", zap.Error(err))
		return err
	}
	logger.Info("execution client: connected")
	ec.conn = conn
	return nil
}

// reconnect tries to reconnect multiple times with an exponent interval
func (ec *eth1Client) reconnect(logger *zap.Logger) {
	limit := 64 * time.Second
	tasks.ExecWithInterval(func(lastTick time.Duration) (stop bool, cont bool) {
		logger.Info("reconnecting")
		if err := ec.connect(logger); err != nil {
			// continue until reaching to limit, and then panic as eth1 connection is required
			if lastTick >= limit {
				logger.Panic("failed to reconnect", zap.Error(err))
			} else {
				logger.Warn("could not reconnect, still trying", zap.Error(err))
			}
			return false, false
		}
		return true, false
	}, 1*time.Second, limit+(1*time.Second))
	logger.Debug("managed to reconnect")
	if err := ec.streamSmartContractEvents(logger); err != nil {
		logger.Panic("failed to stream events after reconnection", zap.Error(err))
	}
}

// fireEvent notifies observers about some contract event
func (ec *eth1Client) fireEvent(log types.Log, name string, data interface{}) {
	e := eth1.Event{Log: log, Name: name, Data: data}
	_ = ec.eventsFeed.Send(&e)
	// TODO: add trace
	// logger.Debug("events was sent to subscribers", zap.Int("num of subscribers", n))
}

// streamSmartContractEvents sync events history of the given contract
func (ec *eth1Client) streamSmartContractEvents(logger *zap.Logger) error {
	currentBlock, err := ec.conn.BlockNumber(ec.ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get current block")
	}
	logger.Debug("streaming smart contract events",
		zap.Uint64("current_block", currentBlock))

	contractAbi, err := abi.JSON(strings.NewReader(ec.contractABI))
	if err != nil {
		return errors.Wrap(err, "failed to parse ABI interface")
	}

	sub, logs, err := ec.subscribeToLogs(logger)
	if err != nil {
		return errors.Wrap(err, "Failed to subscribe to logs")
	}

	go func() {
		if err := ec.listenToSubscription(logger, logs, sub, contractAbi); err != nil {
			ec.reconnect(logger)
		}
	}()

	return nil
}

func (ec *eth1Client) subscribeToLogs(logger *zap.Logger) (ethereum.Subscription, chan types.Log, error) {
	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}
	logs := make(chan types.Log)
	sub, err := ec.conn.SubscribeFilterLogs(ec.ctx, query, logs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to subscribe to logs")
	}
	logger.Debug("subscribed to results of the streaming filter query")

	return sub, logs, nil
}

// listenToSubscription listen to new event logs from the contract
func (ec *eth1Client) listenToSubscription(logger *zap.Logger, logs chan types.Log, sub ethereum.Subscription, contractAbi abi.ABI) error {
	for {
		select {
		case err := <-sub.Err():
			logger.Warn("failed to read logs from subscription", zap.Error(err))
			return err
		case vLog := <-logs:
			if vLog.Removed {
				continue
			}
			logger.Debug("received contract event from stream")
			eventName, err := ec.handleEvent(logger, vLog, contractAbi)
			if err != nil {
				logger.Warn("could not parse ongoing event, the event is malformed",
					fields.EventName(eventName),
					fields.BlockNumber(vLog.BlockNumber),
					fields.TxHash(vLog.TxHash),
					zap.Error(err),
				)
				continue
			}
		}
	}
}

// syncSmartContractsEvents sync events history of the given contract
func (ec *eth1Client) syncSmartContractsEvents(logger *zap.Logger, fromBlock *big.Int) error {
	logger.Debug("syncing smart contract events", fields.FromBlock(fromBlock))

	contractAbi, err := abi.JSON(strings.NewReader(ec.contractABI))
	if err != nil {
		return errors.Wrap(err, "failed to parse ABI interface")
	}
	currentBlock, err := ec.conn.BlockNumber(ec.ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get current block")
	}
	var logs []types.Log
	var nSuccess int
	for {
		var toBlock *big.Int
		if currentBlock-fromBlock.Uint64() > blocksInBatch {
			toBlock = big.NewInt(int64(fromBlock.Uint64() + blocksInBatch))
		} else { // no more batches are required -> setting toBlock to nil
			toBlock = nil
		}
		_logs, _nSuccess, err := ec.fetchAndProcessEvents(logger, fromBlock, toBlock, contractAbi)
		if err != nil {
			// in case request exceeded limit, try again with less blocks
			// will stop after log(blocksInBatch) tries
			if !strings.Contains(err.Error(), "websocket: read limit exceeded") {
				return errors.Wrap(err, "failed to get events")
			}
			currentBatchSize := int64(blocksInBatch)
		retryLoop:
			for currentBatchSize > 1 {
				currentBatchSize /= 2
				logger.Debug("using a lower batch size", zap.Int64("currentBatchSize", currentBatchSize))
				toBlock = big.NewInt(int64(fromBlock.Uint64()) + currentBatchSize)
				_logs, _nSuccess, err = ec.fetchAndProcessEvents(logger, fromBlock, toBlock, contractAbi)
				if err != nil {
					if !strings.Contains(err.Error(), "websocket: read limit exceeded") {
						return errors.Wrap(err, "failed to get events")
					}
					// limit exceeded
					continue retryLoop
				}
				// done
				break retryLoop
			}
		}
		nSuccess += _nSuccess
		logs = append(logs, _logs...)
		if toBlock == nil { // finished
			break
		}
		fromBlock = toBlock
	}
	logger.Debug("finished syncing registry contract",
		zap.Int("total events", len(logs)), zap.Int("total success", nSuccess))
	// publishing SyncEndedEvent so other components could track the sync
	ec.fireEvent(types.Log{}, "SyncEndedEvent", eth1.SyncEndedEvent{Logs: logs, Success: nSuccess == len(logs)})

	return nil
}

func (ec *eth1Client) fetchAndProcessEvents(logger *zap.Logger, fromBlock, toBlock *big.Int, contractAbi abi.ABI) ([]types.Log, int, error) {
	logger = logger.With(fields.FromBlock(fromBlock))
	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
		FromBlock: fromBlock,
	}
	if toBlock != nil {
		query.ToBlock = toBlock
		logger = logger.With(fields.ToBlock(toBlock))
	}
	logger.Debug("fetching event logs")
	logs, err := ec.conn.FilterLogs(ec.ctx, query)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get event logs")
	}
	nSuccess := len(logs)
	logger = logger.With(zap.Int("results", len(logs)))
	logger.Debug("got event logs")

	for _, vLog := range logs {
		eventName, err := ec.handleEvent(logger, vLog, contractAbi)
		if err != nil {
			loggerWith := logger.With(
				fields.EventName(eventName),
				fields.BlockNumber(vLog.BlockNumber),
				fields.TxHash(vLog.TxHash),
				zap.Error(err),
			)
			var malformedEventErr *abiparser.MalformedEventError
			if errors.As(err, &malformedEventErr) {
				loggerWith.Warn("could not parse history sync event, the event is malformed")
			} else {
				loggerWith.Error("could not parse history sync event")
				nSuccess--
			}
			continue
		}
	}
	logger.Debug("event logs were received and parsed successfully",
		zap.Int("successCount", nSuccess))

	return logs, nSuccess, nil
}

func (ec *eth1Client) handleEvent(logger *zap.Logger, vLog types.Log, contractAbi abi.ABI) (string, error) {
	ev, err := contractAbi.EventByID(vLog.Topics[0])
	if err != nil { // unknown event -> ignored
		logger.Debug("could not read event by ID",
			fields.EventID(vLog.Topics[0]),
			fields.BlockNumber(vLog.BlockNumber),
			fields.TxHash(vLog.TxHash),
			zap.Error(err),
		)
		return "", nil
	}

	abiParser := eth1.NewParser(logger, ec.abiVersion)

	switch ev.Name {
	case abiparser.OperatorAdded:
		parsed, err := abiParser.ParseOperatorAddedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.OperatorRemoved:
		_, err = abiParser.ParseOperatorRemovedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		// skip
		//ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.ValidatorAdded:
		parsed, err := abiParser.ParseValidatorAddedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.ValidatorRemoved:
		parsed, err := abiParser.ParseValidatorRemovedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.ClusterLiquidated:
		parsed, err := abiParser.ParseClusterLiquidatedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.ClusterReactivated:
		parsed, err := abiParser.ParseClusterReactivatedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)
	case abiparser.FeeRecipientAddressUpdated:
		parsed, err := abiParser.ParseFeeRecipientAddressUpdatedEvent(vLog, contractAbi)
		reportSyncEvent(ev.Name, err)
		if err != nil {
			return ev.Name, err
		}
		ec.fireEvent(vLog, ev.Name, *parsed)

	default:
		logger.Debug("unsupported contract event was received, skipping",
			zap.String("eventName", ev.Name),
			fields.BlockNumber(vLog.BlockNumber),
			fields.TxHash(vLog.TxHash),
		)
	}
	return ev.Name, nil
}
