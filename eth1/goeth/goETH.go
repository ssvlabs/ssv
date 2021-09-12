package goeth

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
	"math/big"
	"strings"
	"time"
)

const (
	healthCheckTimeout        = 10 * time.Second
	blocksInBatch      uint64 = 100000
)

type eth1NodeStatus int32

var (
	metricsEth1NodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:eth1:node_status",
		Help: "Status of the connected eth1 node",
	})
	statusUnknown eth1NodeStatus = 0
	statusSyncing eth1NodeStatus = 1
	statusOK      eth1NodeStatus = 2
)

func init() {
	if err := prometheus.Register(metricsEth1NodeStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// ClientOptions are the options for the client
type ClientOptions struct {
	Ctx                        context.Context
	Logger                     *zap.Logger
	NodeAddr                   string
	RegistryContractAddr       string
	ContractABI                string
	ConnectionTimeout          time.Duration
	ShareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
}

// eth1Client is the internal implementation of Client
type eth1Client struct {
	ctx    context.Context
	conn   *ethclient.Client
	logger *zap.Logger

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider

	nodeAddr             string
	registryContractAddr string
	contractABI          string
	connectionTimeout    time.Duration

	outSubject pubsub.Subject
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &eth1Client{}

// NewEth1Client creates a new instance
func NewEth1Client(opts ClientOptions) (eth1.Client, error) {
	logger := opts.Logger.With(zap.String("component", "eth1GoETH"))
	logger.Info("eth1 addresses", zap.String("address", opts.NodeAddr), zap.String("contract", opts.RegistryContractAddr))

	ec := eth1Client{
		ctx:                        opts.Ctx,
		logger:                     logger,
		shareEncryptionKeyProvider: opts.ShareEncryptionKeyProvider,
		nodeAddr:                   opts.NodeAddr,
		registryContractAddr:       opts.RegistryContractAddr,
		contractABI:                opts.ContractABI,
		connectionTimeout:          opts.ConnectionTimeout,
		outSubject:                 pubsub.NewSubject(logger),
	}

	if err := ec.connect(); err != nil {
		logger.Error("failed to connect to the Ethereum client", zap.Error(err))
		return nil, err
	}

	return &ec, nil
}

// EventsSubject returns the events subject
func (ec *eth1Client) EventsSubject() pubsub.Subscriber {
	return ec.outSubject
}

// Start streams events from the contract
func (ec *eth1Client) Start() error {
	err := ec.streamSmartContractEvents()
	if err != nil {
		ec.logger.Error("Failed to init operator contract address subject", zap.Error(err))
	}
	return err
}

// Sync reads events history
func (ec *eth1Client) Sync(fromBlock *big.Int) error {
	err := ec.syncSmartContractsEvents(fromBlock)
	if err != nil {
		ec.logger.Error("Failed to sync contract events", zap.Error(err))
	}
	return err
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
		metricsEth1NodeStatus.Set(float64(statusUnknown))
		return []string{"could not get eth1 node sync progress"}
	}
	if sp != nil {
		metricsEth1NodeStatus.Set(float64(statusSyncing))
		return []string{fmt.Sprintf("eth1 node is currently syncing: starting=%d, current=%d, highest=%d",
			sp.StartingBlock, sp.CurrentBlock, sp.HighestBlock)}
	}
	// eth1 node is connected and synced
	metricsEth1NodeStatus.Set(float64(statusOK))
	return []string{}
}

// connect connects to eth1 client
func (ec *eth1Client) connect() error {
	// Create an IPC based RPC connection to a remote node
	ec.logger.Info("dialing eth1 node...")
	ctx, cancel := context.WithTimeout(context.Background(), ec.connectionTimeout)
	defer cancel()
	conn, err := ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		ec.logger.Error("could not connect to the eth1 client", zap.Error(err))
		return err
	}
	ec.logger.Info("successfully connected to eth1 goETH")
	ec.conn = conn
	return nil
}

// reconnect tries to reconnect multiple times with an exponent interval
func (ec *eth1Client) reconnect() {
	limit := 64 * time.Second
	tasks.ExecWithInterval(func(lastTick time.Duration) (stop bool, cont bool) {
		ec.logger.Info("reconnecting to eth1 node")
		if err := ec.connect(); err != nil {
			// continue until reaching to limit, and then panic as eth1 connection is required
			if lastTick >= limit {
				ec.logger.Panic("failed to reconnect to eth1 node", zap.Error(err))
			} else {
				ec.logger.Warn("could not reconnect to eth1 node, still trying", zap.Error(err))
			}
			return false, false
		}
		return true, false
	}, 1*time.Second, limit+(1*time.Second))
	ec.logger.Debug("managed to reconnect to eth1 node")
	if err := ec.streamSmartContractEvents(); err != nil {
		// TODO: panic?
		ec.logger.Error("failed to stream events after reconnection", zap.Error(err))
	}
}

// fireEvent notifies observers about some contract event
func (ec *eth1Client) fireEvent(log types.Log, data interface{}) {
	e := eth1.Event{Log: log, Data: data}
	ec.outSubject.Notify(e)
}

// streamSmartContractEvents sync events history of the given contract
func (ec *eth1Client) streamSmartContractEvents() error {
	ec.logger.Debug("streaming smart contract events")

	contractAbi, err := abi.JSON(strings.NewReader(ec.contractABI))
	if err != nil {
		return errors.Wrap(err, "failed to parse ABI interface")
	}

	sub, logs, err := ec.subscribeToLogs()
	if err != nil {
		return errors.Wrap(err, "Failed to subscribe to logs")
	}

	go func() {
		if err := ec.listenToSubscription(logs, sub, contractAbi); err != nil {
			ec.reconnect()
		}
	}()

	return nil
}

func (ec *eth1Client) subscribeToLogs() (ethereum.Subscription, chan types.Log, error) {
	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}
	logs := make(chan types.Log)
	sub, err := ec.conn.SubscribeFilterLogs(ec.ctx, query, logs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to subscribe to logs")
	}
	ec.logger.Debug("subscribed to results of the streaming filter query")

	return sub, logs, nil
}

// listenToSubscription listen to new event logs from the contract
func (ec *eth1Client) listenToSubscription(logs chan types.Log, sub ethereum.Subscription, contractAbi abi.ABI) error {
	for {
		select {
		case err := <-sub.Err():
			ec.logger.Warn("failed to read logs from subscription", zap.Error(err))
			return err
		case vLog := <-logs:
			ec.logger.Debug("received contract event from stream")
			err := ec.handleEvent(vLog, contractAbi)
			if err != nil {
				ec.logger.Error("Failed to handle event", zap.Error(err))
				continue
			}
		}
	}
}

// syncSmartContractsEvents sync events history of the given contract
func (ec *eth1Client) syncSmartContractsEvents(fromBlock *big.Int) error {
	ec.logger.Debug("syncing smart contract events",
		zap.Uint64("fromBlock", fromBlock.Uint64()))

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
		_logs, _nSuccess, err := ec.fetchAndProcessEvents(fromBlock, toBlock, contractAbi)
		if err != nil {
			return errors.Wrap(err, "failed to get events")
		}
		nSuccess += _nSuccess
		logs = append(logs, _logs...)
		if toBlock == nil { // finished
			break
		}
		fromBlock = toBlock
	}

	ec.logger.Debug("finished syncing registry contract",
		zap.Int("total events", len(logs)), zap.Int("total success", nSuccess))
	// publishing SyncEndedEvent so other components could track the sync
	ec.fireEvent(types.Log{}, eth1.SyncEndedEvent{Logs: logs, Success: nSuccess == len(logs)})

	return nil
}

func (ec *eth1Client) fetchAndProcessEvents(fromBlock, toBlock *big.Int, contractAbi abi.ABI) ([]types.Log, int, error) {
	logger := ec.logger.With(zap.Int64("fromBlock", fromBlock.Int64()))
	if toBlock != nil {
		logger = logger.With(zap.Int64("toBlock", toBlock.Int64()))
	}
	logger.Debug("fetching event logs")

	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
		FromBlock: fromBlock,
	}
	if toBlock != nil {
		query.ToBlock = toBlock
	}
	logs, err := ec.conn.FilterLogs(ec.ctx, query)
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get event logs")
	}
	nSuccess := len(logs)
	logger = logger.With(zap.Int("results", len(logs)))
	logger.Debug("got event logs")

	for _, vLog := range logs {
		err := ec.handleEvent(vLog, contractAbi)
		if err != nil {
			nSuccess--
			ec.logger.Error("Failed to handle event during sync", zap.Error(err))
			continue
		}
	}
	logger.Debug("event logs were received and parsed successfully",
		zap.Int("successCount", nSuccess))

	return logs, nSuccess, nil
}

func (ec *eth1Client) handleEvent(vLog types.Log, contractAbi abi.ABI) error {
	ec.logger.Debug("handling smart contract event", zap.Any("vLog", vLog))

	eventType, err := contractAbi.EventByID(vLog.Topics[0])
	if err != nil { // unknown event -> ignored
		ec.logger.Warn("failed to find event type", zap.Error(err), zap.String("txHash", vLog.TxHash.Hex()))
		return nil
	}
	shareEncryptionKey, found, err := ec.shareEncryptionKeyProvider()
	if !found {
		return errors.New("failed to find operator private key")
	}
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}

	switch eventName := eventType.Name; eventName {
	case "OperatorAdded":
		parsed, isEventBelongsToOperator, err := eth1.ParseOperatorAddedEvent(ec.logger, shareEncryptionKey, vLog.Data, contractAbi)
		if err != nil {
			return errors.Wrap(err, "failed to parse OperatorAdded event")
		}
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || shareEncryptionKey == nil {
			ec.fireEvent(vLog, *parsed)
		}
	case "ValidatorAdded":
		parsed, isEventBelongsToOperator, err := eth1.ParseValidatorAddedEvent(ec.logger, shareEncryptionKey, vLog.Data, contractAbi)
		if err != nil {
			return errors.Wrap(err, "failed to parse ValidatorAdded event")
		}
		if !isEventBelongsToOperator {
			ec.logger.Debug("Validator doesn't belong to operator",
				zap.String("pubKey", hex.EncodeToString(parsed.PublicKey)))
		}
		ec.logger.Debug("parsed data",
			zap.String("pubKey", hex.EncodeToString(parsed.PublicKey)), zap.Any("parsed", parsed))
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || shareEncryptionKey == nil {
			ec.fireEvent(vLog, *parsed)
		}
	default:
		ec.logger.Debug("unknown contract event was received")
	}
	return nil
}
