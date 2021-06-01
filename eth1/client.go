package eth1

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/shared/params"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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

// Event represents an eth1 event log in the system
type Event struct {
	Log  types.Log
	Data interface{}
}
// SyncEndedEvent meant to notify an observer that the sync is over
type SyncEndedEvent struct{}
// OperatorPrivateKeyProvider is a function that returns the operator private key
type OperatorPrivateKeyProvider = func() (*rsa.PrivateKey, error)

// Client represents the required interface for eth1 client
type Client interface {
	Subject() pubsub.SubjectBase
	Start() error
	Sync(fromBlock *big.Int) error
}

// eth1Client is the internal implementation of Client
type eth1Client struct {
	ctx    context.Context
	conn   *ethclient.Client
	logger *zap.Logger

	operatorPrivKeyProvider OperatorPrivateKeyProvider

	outSubject pubsub.Subject
}

// ClientOptions are the options for the client
type ClientOptions struct {
	Ctx             context.Context
	Logger          *zap.Logger
	NodeAddr        string
	PrivKeyProvider OperatorPrivateKeyProvider
}

// NewEth1Client creates a new instance
func NewEth1Client(opts ClientOptions) (Client, error) {
	logger := opts.Logger
	// Create an IPC based RPC connection to a remote node
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
		outSubject:              pubsub.NewSubject(),
	}

	return &ec, nil
}

// Subject returns the events subject
func (ec *eth1Client) Subject() pubsub.SubjectBase {
	return ec.outSubject
}

// Start streams events from the contract
func (ec *eth1Client) Start() error {
	err := ec.streamSmartContractEvents(params.SsvConfig().OperatorContractAddress, params.SsvConfig().ContractABI)
	if err != nil {
		ec.logger.Error("Failed to init operator contract address subject", zap.Error(err))
	}
	return err
}

// Sync reads events history
func (ec *eth1Client) Sync(fromBlock *big.Int) error {
	err := ec.syncSmartContractsEvents(params.SsvConfig().OperatorContractAddress, params.SsvConfig().ContractABI, fromBlock)
	if err != nil {
		ec.logger.Error("Failed to sync contract events", zap.Error(err))
	}
	return err
}

// publishEvents notifies observers
func (ec *eth1Client) publishEvent(log types.Log, data interface{}) {
	e := Event{log, data}
	ec.outSubject.Notify(e)
}

// streamSmartContractEvents sync events history of the given contract
func (ec *eth1Client) streamSmartContractEvents(contractAddr, contractABI string) error {
	ec.logger.Debug("streaming smart contract events")

	contractAbi, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return errors.Wrap(err, "failed to parse ABI interface")
	}

	contractAddress := common.HexToAddress(contractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}
	logs := make(chan types.Log)
	sub, err := ec.conn.SubscribeFilterLogs(ec.ctx, query, logs)
	if err != nil {
		//ec.logger.Fatal("Failed to subscribe to logs", zap.Error(err))
		return errors.Wrap(err, "Failed to subscribe to logs")
	}

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
			err := ec.handleEvent(vLog, contractAbi)
			if err != nil {
				ec.logger.Error("Failed to handle event", zap.Error(err))
				continue
			}
		}
	}
}

// syncSmartContractsEvents sync events history of the given contract
func (ec *eth1Client) syncSmartContractsEvents(contractAddr, contractABI string, fromBlock *big.Int) error {
	ec.logger.Debug("syncing smart contract events")

	contractAbi, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return errors.Wrap(err, "failed to parse ABI interface")
	}
	contractAddress := common.HexToAddress(contractAddr)
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
	ec.logger.Debug(fmt.Sprintf("%d event logs were handled successfuly", nResults))
	// publishing SyncEndedEvent so other components could track the sync
	ec.publishEvent(types.Log{}, SyncEndedEvent{})

	return nil
}

func (ec *eth1Client) handleEvent(vLog types.Log, contractAbi abi.ABI) error {
	eventType, err := contractAbi.EventByID(vLog.Topics[0])
	if err != nil {
		return errors.Wrap(err, "Failed to find event type")
	}
	operatorPriveKey, err := ec.operatorPrivKeyProvider()
	if err != nil {
		return errors.Wrap(err, "Failed to get operator private key")
	}

	switch eventName := eventType.Name; eventName {
	case "OperatorAdded":
		parsed, isEventBelongsToOperator, err := ec.parseOperatorAddedEvent(vLog.Data, contractAbi, eventName)
		if err != nil {
			//ec.logger.Error("Failed to parse OperatorAdded event", zap.Error(err))
			return errors.Wrap(err, "Failed to parse OperatorAdded event")
		}
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || operatorPriveKey == nil {
			ec.publishEvent(vLog, *parsed)
		}
	case "ValidatorAdded":
		event, isEventBelongsToOperator, err := ec.parseValidatorAddedEvent(vLog.Data, contractAbi, eventName)
		if err != nil {
			//ec.logger.Error("Failed to parse ValidatorAdded event", zap.Error(err))
			return errors.Wrap(err, "Failed to parse ValidatorAdded event")
		}
		if !isEventBelongsToOperator {
			ec.logger.Debug("ValidatorAdded Event doesn't belong to operator")
		}
		// if there is no operator-private-key --> assuming that the event should be triggered (e.g. exporter)
		if isEventBelongsToOperator || operatorPriveKey == nil {
			ec.publishEvent(vLog, *event)
		}
	default:
		ec.logger.Debug("Unknown contract event is received")
	}
	return nil
}

// parseOperatorAddedEvent parses an OperatorAddedEvent
func (ec *eth1Client) parseOperatorAddedEvent(data []byte, contractAbi abi.ABI, eventName string) (*OperatorAddedEvent, bool, error) {
	var operatorAddedEvent OperatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&operatorAddedEvent, eventName, data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack OperatorAdded event")
	}
	operatorPubkeyHex := hex.EncodeToString(operatorAddedEvent.PublicKey)
	ec.logger.Debug("OperatorAdded Event",
		zap.String("Operator PublicKey", operatorPubkeyHex),
		zap.String("Payment Address", operatorAddedEvent.PaymentAddress.String()))
	isEventBelongsToOperator := strings.EqualFold(operatorPubkeyHex, params.SsvConfig().OperatorPublicKey)
	return &operatorAddedEvent, isEventBelongsToOperator, nil
}

// parseValidatorAddedEvent parses ValidatorAddedEvent
func (ec *eth1Client) parseValidatorAddedEvent(data []byte, contractAbi abi.ABI, eventName string) (*ValidatorAddedEvent, bool, error) {
	var validatorAddedEvent ValidatorAddedEvent
	err := contractAbi.UnpackIntoInterface(&validatorAddedEvent, eventName, data)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed to unpack ValidatorAdded event")
	}

	ec.logger.Debug("ValidatorAdded Event",
		zap.String("Validator PublicKey", hex.EncodeToString(validatorAddedEvent.PublicKey)),
		zap.String("Owner Address", validatorAddedEvent.OwnerAddress.String()))

	var isEventBelongsToOperator bool

	for i := range validatorAddedEvent.OessList {
		validatorShare := &validatorAddedEvent.OessList[i]

		def := `[{ "name" : "method", "type": "function", "outputs": [{"type": "string"}]}]` //TODO need to set as var?
		outAbi, err := abi.JSON(strings.NewReader(def))
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to define ABI")
		}

		outOperatorPublicKey, err := outAbi.Unpack("method", validatorShare.OperatorPublicKey)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to unpack OperatorPublicKey")
		}

		if operatorPublicKey, ok := outOperatorPublicKey[0].(string); ok {
			validatorShare.OperatorPublicKey = []byte(operatorPublicKey) // set for further use in code
			if strings.EqualFold(operatorPublicKey, params.SsvConfig().OperatorPublicKey) {
				sk, err := ec.operatorPrivKeyProvider()
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to get private key")
				}
				if sk == nil {
					continue
				}

				out, err := outAbi.Unpack("method", validatorShare.EncryptedKey)
				if err != nil {
					return nil, false, errors.Wrap(err, "failed to unpack EncryptedKey")
				}

				if encryptedSharePrivateKey, ok := out[0].(string); ok {
					decryptedSharePrivateKey, err := rsaencryption.DecodeKey(sk, encryptedSharePrivateKey)
					decryptedSharePrivateKey = strings.Replace(decryptedSharePrivateKey, "0x", "", 1)
					if err != nil {
						return nil, false, errors.Wrap(err, "failed to decrypt share private key")
					}
					validatorShare.EncryptedKey = []byte(decryptedSharePrivateKey)
					isEventBelongsToOperator = true
				}
			}
		}
	}

	return &validatorAddedEvent, isEventBelongsToOperator, nil
}
