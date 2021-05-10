package goeth

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/shared/params"
)

type eth1GRPC struct {
	ctx           context.Context
	conn          *ethclient.Client
	logger        *zap.Logger
	contractEvent *eth1.ContractEvent
}

// New create new goEth instance
func New(ctx context.Context, logger *zap.Logger, nodeAddr string) (eth1.Eth1, error) {
	// Create an IPC based RPC connection to a remote node
	conn, err := ethclient.Dial(nodeAddr)
	if err != nil {
		logger.Error("Failed to connect to the Ethereum client", zap.Error(err))
	}

	e := &eth1GRPC{
		ctx:    ctx,
		conn:   conn,
		logger: logger,
	}

	// init the instance which publishes an event when anything happens
	err = e.streamSmartContractEvents(params.SsvConfig().OperatorContractAddress)
	if err != nil {
		logger.Error("Failed to init operator contract address subject", zap.Error(err))
	}

	return e, nil
}

// streamSmartContractEvents implements Eth1 interface
func (e *eth1GRPC) streamSmartContractEvents(contractAddr string) error {
	contractAddress := common.HexToAddress(contractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}

	logs := make(chan types.Log)
	sub, err := e.conn.SubscribeFilterLogs(e.ctx, query, logs)
	if err != nil {
		e.logger.Fatal("Failed to subscribe to logs", zap.Error(err))
		return err
	}

	contractAbi, err := abi.JSON(strings.NewReader(params.SsvConfig().ContractABI))
	if err != nil {
		e.logger.Fatal("Failed to parse ABI interface", zap.Error(err))
	}

	e.contractEvent = eth1.NewContractEvent("smartContractEvent")
	go func() {
		for {
			select {
			case err := <-sub.Err():
				// TODO might fail consider reconnect
				e.logger.Error("Error from logs sub", zap.Error(err))

			case vLog := <-logs:
				eventType, err := contractAbi.EventByID(vLog.Topics[0])
				if err != nil {
					e.logger.Error("Failed to get event by topic hash", zap.Error(err))
					continue
				}

				switch eventName := eventType.Name; eventName {
				case "OperatorAdded":
					operatorAddedEvent := struct {
						Name           string
						Pubkey         []byte
						PaymentAddress common.Address
					}{}
					err = contractAbi.UnpackIntoInterface(&operatorAddedEvent, eventType.Name, vLog.Data)
					if err != nil {
						e.logger.Error("Failed to unpack event", zap.Error(err))
						continue
					}
					e.contractEvent.Data = operatorAddedEvent

				case "ValidatorAdded":
					validatorAddedEvent := struct {
						Pubkey       []byte
						OwnerAddress common.Address
						Oess         []byte
					}{}
					err = contractAbi.UnpackIntoInterface(&validatorAddedEvent, eventType.Name, vLog.Data)
					if err != nil {
						e.logger.Error("Failed to unpack ValidatorAdded event", zap.Error(err))
						continue
					}

					oessAbi, err := abi.JSON(strings.NewReader(params.SsvConfig().OessABI))
					if err != nil {
						e.logger.Fatal("Failed to parse Oess ABI interface", zap.Error(err))
						continue
					}

					type oess struct {
						OperatorPubKey []byte
						Index          *big.Int
						SharePubKey    []byte
						EncryptedKey   []byte
					}

					oessListEncoded := deleteEmpty(strings.Split(hex.EncodeToString(validatorAddedEvent.Oess), params.SsvConfig().OessSeparator))
					oessList := make([]oess, len(oessListEncoded))
					isEventBelongsToOperator := false

					e.logger.Debug("Validator PubKey:", zap.String("", hex.EncodeToString(validatorAddedEvent.Pubkey)))
					e.logger.Debug("Owner Address:   ", zap.String("", validatorAddedEvent.OwnerAddress.String()))
					for i := range oessListEncoded {
						oessEncoded, err := hex.DecodeString(oessListEncoded[i])
						if err != nil {
							e.logger.Error("Failed to HEX decode Oess", zap.Error(err))
							continue
						}

						err = oessAbi.UnpackIntoInterface(&oessList[i], "tuple", oessEncoded)
						if err != nil {
							e.logger.Error("Failed to unpack Oess struct", zap.Error(err))
							continue
						}

						e.logger.Debug("Index:           ", zap.Any("", oessList[i].Index))
						e.logger.Debug("Operator PubKey: ", zap.String("", hex.EncodeToString(oessList[i].OperatorPubKey)))
						e.logger.Debug("Share PubKey:    ", zap.String("", hex.EncodeToString(oessList[i].SharePubKey)))
						e.logger.Debug("Encrypted Key:   ", zap.String("", hex.EncodeToString(oessList[i].EncryptedKey)))

						if strings.EqualFold(hex.EncodeToString(oessList[i].OperatorPubKey), params.SsvConfig().OperatorPublicKey) {
							isEventBelongsToOperator = true
						}
					}

					if isEventBelongsToOperator {
						e.contractEvent.Data = oessList
						e.contractEvent.NotifyAll()
					}

				default:
					e.logger.Debug("Unknown contract event is received")
					continue
				}
			}
		}
	}()
	return nil
}

func (e *eth1GRPC) GetContractEvent() *eth1.ContractEvent {
	return e.contractEvent
}

func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}
