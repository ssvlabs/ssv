package goeth

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
	"strings"
	"time"
)

// ClientOptions are the options for the client
type ClientOptions struct {
	Ctx                        context.Context
	Logger                     *zap.Logger
	NodeAddr                   string
	RegistryContractAddr       string
	ContractABI                string
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

	outSubject pubsub.Subject
}

// NewEth1Client creates a new instance
func NewEth1Client(opts ClientOptions) (eth1.Client, error) {
	logger := opts.Logger

	ec := eth1Client{
		ctx:                        opts.Ctx,
		logger:                     logger,
		shareEncryptionKeyProvider: opts.ShareEncryptionKeyProvider,
		nodeAddr:                   opts.NodeAddr,
		registryContractAddr:       opts.RegistryContractAddr,
		contractABI:                opts.ContractABI,
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

// connect connects to eth1 client
func (ec *eth1Client) connect() error {
	// Create an IPC based RPC connection to a remote node
	ec.logger.Info("dialing node", zap.String("addr", ec.nodeAddr))
	conn, err := ethclient.Dial(ec.nodeAddr)
	if err != nil {
		ec.logger.Error("failed to reconnect to the Ethereum client", zap.Error(err))
		return err
	}
	ec.conn = conn
	return nil
}

// reconnect tries to reconnect multiple times
func (ec *eth1Client) reconnect() {
	limit := 64 * time.Second
	tasks.ExecWithInterval(func(lastTick time.Duration) (stop bool, cont bool) {
		ec.logger.Info("reconnecting to eth1 node")
		if err := ec.connect(); err != nil {
			// once getting to limit, panic as the node should have an open eth1 connection to be aligned
			if lastTick >= limit {
				ec.logger.Panic("failed to reconnect to eth1 node", zap.Error(err))
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
		err := ec.listenToSubscription(logs, sub, contractAbi)
		// in case of disconnection try to reconnect
		if isCloseError(err) {
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

func isCloseError(err error) bool {
	_, ok := err.(*websocket.CloseError)
	return ok
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
	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
		FromBlock: fromBlock,
	}
	logs, err := ec.conn.FilterLogs(ec.ctx, query)
	if err != nil {
		return errors.Wrap(err, "failed to get event logs")
	}
	nResults := len(logs)
	ec.logger.Debug(fmt.Sprintf("got event logs, number of results: %d", nResults))

	for _, vLog := range logs {
		err := ec.handleEvent(vLog, contractAbi)
		if err != nil {
			nResults--
			ec.logger.Error("Failed to handle event during sync", zap.Error(err))
			continue
		}
	}
	ec.logger.Debug(fmt.Sprintf("%d event logs were received and parsed successfully", nResults))
	// publishing SyncEndedEvent so other components could track the sync
	ec.fireEvent(types.Log{}, eth1.SyncEndedEvent{Logs: logs, Success: nResults == len(logs)})

	return nil
}

func (ec *eth1Client) handleEvent(vLog types.Log, contractAbi abi.ABI) error {
	ec.logger.Debug("handling smart contract event")

	eventType, err := contractAbi.EventByID(vLog.Topics[0])
	if err != nil { // unknown event -> ignored
		ec.logger.Warn("failed to find event type", zap.Error(err), zap.String("txHash", vLog.TxHash.Hex()))
		return nil
	}
	shareEncryptionKey, err := ec.shareEncryptionKeyProvider()
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
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || shareEncryptionKey == nil {
			ec.fireEvent(vLog, *parsed)
		}
	default:
		ec.logger.Debug("unknown contract event was received")
	}
	return nil
}
