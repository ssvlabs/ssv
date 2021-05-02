package goeth

import (
	"github.com/bloxapp/ssv/eth1"
	"github.com/pkg/errors"

	"context"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

type eth1GRPC struct {
	ctx    context.Context
	conn   *ethclient.Client
	logger *zap.Logger
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
	}

	return e, nil
}

// StreamSmartContractEvents implements Eth1 interface
func (e *eth1GRPC) StreamSmartContractEvents(contractAddr string) (*eth1.Event, error) {
	contractAddress := common.HexToAddress(contractAddr)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{contractAddress},
	}

	logs := make(chan types.Log)
	sub, err := e.conn.SubscribeFilterLogs(e.ctx, query, logs)
	if err != nil {
		e.logger.Fatal("Failed to subscribe to logs", zap.Error(err))
	}

	event := eth1.NewEvent("smartContractEvent")
	go func() {
		for {
			select {
			case err := <-sub.Err():
				e.logger.Fatal("Error from logs sub", zap.Error(err))
			case vLog := <-logs:
				event.Log = vLog
				event.NotifyAll()
			}
		}
	}()

	return event, nil
}

