package goeth

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"math/big"
	"strings"
)

// ClientOptions are the options for the client
type ClientOptions struct {
	Ctx                  context.Context
	Logger               *zap.Logger
	NodeAddr             string
	RegistryContractAddr string
	ContractABI          string
	PrivKeyProvider      eth1.OperatorPrivateKeyProvider
}

// eth1Client is the internal implementation of Client
type eth1Client struct {
	ctx    context.Context
	conn   *ethclient.Client
	logger *zap.Logger

	operatorPrivKeyProvider eth1.OperatorPrivateKeyProvider

	registryContractAddr string
	contractABI          string

	outSubject pubsub.Subject
}

// NewEth1Client creates a new instance
func NewEth1Client(opts ClientOptions) (eth1.Client, error) {
	logger := opts.Logger
	// Create an IPC based RPC connection to a remote node
	logger.Info("dialing node", zap.String("addr", opts.NodeAddr))
	conn, err := ethclient.Dial(opts.NodeAddr)
	if err != nil {
		logger.Error("Failed to connect to the Ethereum client", zap.Error(err))
		return nil, err
	}

	ec := eth1Client{
		ctx:                     opts.Ctx,
		conn:                    conn,
		logger:                  logger,
		operatorPrivKeyProvider: opts.PrivKeyProvider,
		registryContractAddr:    opts.RegistryContractAddr,
		contractABI:             opts.ContractABI,
		outSubject:              pubsub.NewSubject(logger),
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

	contractAddress := common.HexToAddress(ec.registryContractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}
	logs := make(chan types.Log)
	sub, err := ec.conn.SubscribeFilterLogs(ec.ctx, query, logs)
	if err != nil {
		return errors.Wrap(err, "Failed to subscribe to logs")
	}
	ec.logger.Debug("subscribed to results of the streaming filter query")

	go ec.listenToSubscription(logs, sub, contractAbi)

	return nil
}

// listenToSubscription listen to new event logs from the contract
func (ec *eth1Client) listenToSubscription(logs chan types.Log, sub ethereum.Subscription, contractAbi abi.ABI) {
	for {
		select {
		case err := <-sub.Err():
			// TODO might fail consider reconnect
			ec.logger.Error("Error from logs sub", zap.Error(err))
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
	operatorPriveKey, err := ec.operatorPrivKeyProvider()
	if err != nil {
		return errors.Wrap(err, "failed to get operator private key")
	}

	switch eventName := eventType.Name; eventName {
	case "OperatorAdded":
		parsed, isEventBelongsToOperator, err := eth1.ParseOperatorAddedEvent(ec.logger, operatorPriveKey, vLog.Data, contractAbi)
		if err != nil {
			return errors.Wrap(err, "failed to parse OperatorAdded event")
		}
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || operatorPriveKey == nil {
			ec.fireEvent(vLog, *parsed)
		}
	case "ValidatorAdded":
		parsed, isEventBelongsToOperator, err := eth1.ParseValidatorAddedEvent(ec.logger, operatorPriveKey, vLog.Data, contractAbi)
		if err != nil {
			return errors.Wrap(err, "failed to parse ValidatorAdded event")
		}
		if !isEventBelongsToOperator {
			ec.logger.Debug("Validator doesn't belong to operator",
				zap.String("pubKey", hex.EncodeToString(parsed.PublicKey)))
		}
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || operatorPriveKey == nil {
			ec.fireEvent(vLog, *parsed)
		}
	default:
		ec.logger.Debug("unknown contract event was received")
	}
	return nil
}
