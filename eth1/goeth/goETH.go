package goeth

import (
	"context"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
)

type eth1GRPC struct {
	ctx    context.Context
	conn   *ethclient.Client
	logger *zap.Logger
	Event  *eth1.Event
}

// New create new goEth instance
func New(ctx context.Context, logger *zap.Logger, nodeAddr string) (eth1.Eth1, error) {
	// Create an IPC based RPC connection to a remote node
	conn, err := ethclient.Dial(nodeAddr)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to connect to the Ethereum client")
	}

	e := &eth1GRPC{
		ctx:    ctx,
		conn:   conn,
		logger: logger,
		Event: &eth1.Event{},
	}

	return e, nil
}

// StreamSmartContractEvents implements Eth1 interface
func (e *eth1GRPC) StreamSmartContractEvents(contractAddr string) error {
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

	e.Event = eth1.NewEvent("smartContractEvent")
	go func() {
		for {
			select {
			case err := <-sub.Err():
				// TODO might fail consider reconnect
				e.logger.Error("Error from logs sub", zap.Error(err))

			case vLog := <-logs:
				e.Event.Log = vLog
				e.Event.NotifyAll()
			}
		}
	}()
	return nil
}

func (e *eth1GRPC) GetEvent() *eth1.Event {
	return e.Event
}