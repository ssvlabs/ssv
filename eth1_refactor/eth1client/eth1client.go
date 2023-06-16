package eth1client

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/utils/tasks"
)

// Eth1Client represents a client for interacting with eth1 node.
type Eth1Client struct {
	// mandatory
	nodeAddr        string
	contractAddress ethcommon.Address

	// optional
	logger                      *zap.Logger
	metrics                     metrics
	finalizationOffset          uint64
	connectionTimeout           time.Duration
	reconnectionInitialInterval time.Duration
	reconnectionMaxInterval     time.Duration

	// variables
	client           atomic.Pointer[ethclient.Client]
	lastFetchedBlock atomic.Uint64
	closed           chan struct{}
}

// New creates a new instance of Eth1Client.
func New(nodeAddr string, contractAddr ethcommon.Address, opts ...Option) *Eth1Client {
	client := &Eth1Client{
		nodeAddr:                    nodeAddr,
		contractAddress:             contractAddr,
		logger:                      zap.NewNop(),
		metrics:                     nopMetrics{},
		finalizationOffset:          finalizationOffset,
		connectionTimeout:           defaultConnectionTimeout,
		reconnectionInitialInterval: defaultReconnectionInitialInterval,
		reconnectionMaxInterval:     defaultReconnectionMaxInterval,
		closed:                      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Connect connects to eth1 node.
func (ec *Eth1Client) Connect(ctx context.Context) error {
	if ec.client.Load() != nil {
		ec.reconnect(ctx)
		return nil
	}

	if err := ec.connect(ctx); err != nil {
		ec.reconnect(ctx)
	}

	return nil
}

// Close shuts down Eth1Client.
func (ec *Eth1Client) Close() error {
	close(ec.closed)

	if client := ec.client.Load(); client != nil {
		client.Close()
	}

	return nil
}

// FetchHistoricalLogs retrieves historical logs emitted by the contract starting from fromBlock.
func (ec *Eth1Client) FetchHistoricalLogs(ctx context.Context, fromBlock uint64) ([]ethtypes.Log, error) {
	client := ec.client.Load()

	currentBlock, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current block: %w", err)
	}

	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ec.contractAddress},
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(currentBlock - ec.finalizationOffset),
	}

	logs, err := client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetch logs: %w", err)
	}

	ec.setLastFetchedBlock(query.ToBlock.Uint64())

	return logs, nil
}

// StreamLogs subscribes to events emitted by the contract.
func (ec *Eth1Client) StreamLogs(ctx context.Context) <-chan ethtypes.Log {
	logs := make(chan ethtypes.Log)

	go func() {
		defer close(logs)

		for {
			select {
			case <-ctx.Done():
				ec.logger.Debug("log streaming canceled")
				return

			case <-ec.closed:
				ec.logger.Debug("closed")
				return

			default:
				if err := ec.streamLogsToChan(ctx, logs); err != nil {
					ec.logger.Error("log streaming failed", zap.Error(err))
					ec.reconnect(ctx)
				}
			}
		}
	}()

	return logs
}

// IsReady returns if eth1 is currently ready: responds to requests and not in the syncing state.
func (ec *Eth1Client) IsReady(ctx context.Context) (bool, error) {
	if ec.isClosed() {
		return false, nil
	}

	client := ec.client.Load()

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	sp, err := client.SyncProgress(ctx)
	if err != nil {
		ec.metrics.Eth1Failure()
		return false, err
	}

	if sp != nil {
		ec.metrics.Eth1Syncing()
		return false, nil
	}

	ec.metrics.Eth1Ready()

	return true, nil
}

func (ec *Eth1Client) isClosed() bool {
	select {
	case <-ec.closed:
		return true
	default:
		return false
	}
}

func (ec *Eth1Client) streamLogsToChan(ctx context.Context, logs chan ethtypes.Log) error {
	client := ec.client.Load()

	heads := make(chan *ethtypes.Header)

	sub, err := client.SubscribeNewHead(ctx, heads)
	if err != nil {
		return fmt.Errorf("subscribe heads: %w", err)
	}

	for {
		select {
		case err := <-sub.Err():
			return fmt.Errorf("subscription: %w", err)

		case header := <-heads:
			query := ethereum.FilterQuery{
				Addresses: []ethcommon.Address{ec.contractAddress},
				FromBlock: new(big.Int).SetUint64(ec.lastFetchedBlock.Load() + 1),
				ToBlock:   header.Number,
			}

			newLogs, err := client.FilterLogs(ctx, query)
			if err != nil {
				return fmt.Errorf("fetch logs: %w", err)
			}

			for _, log := range newLogs {
				logs <- log
			}

			ec.setLastFetchedBlock(query.ToBlock.Uint64())
		}
	}
}

// connect connects to eth1.
// It must not be called twice in parallel.
func (ec *Eth1Client) connect(ctx context.Context) error {
	logger := ec.logger.With(fields.Address(ec.nodeAddr))

	ctx, cancel := context.WithTimeout(ctx, ec.connectionTimeout)
	defer cancel()

	logger.Info("connecting", zap.Duration("timeout", ec.connectionTimeout))

	client, err := ethclient.DialContext(ctx, ec.nodeAddr)
	if err != nil {
		logger.Error("connection failed", zap.Error(err))
		return err
	}

	logger.Info("connected")
	ec.client.Store(client)

	return nil
}

// reconnect tries to reconnect multiple times with an exponent interval.
// It panics when reconnecting limit is reached.
// It must not be called twice in parallel.
func (ec *Eth1Client) reconnect(ctx context.Context) {
	logger := ec.logger.With(fields.Address(ec.nodeAddr))

	if cl := ec.client.Load(); cl != nil {
		cl.Close()
	}

	tasks.ExecWithInterval(func(lastTick time.Duration) (stop bool, cont bool) {
		logger.Info("reconnecting")
		if err := ec.connect(ctx); err != nil {
			if ec.isClosed() {
				return true, false
			}
			// continue until reaching to limit, and then panic as eth1 connection is required
			if lastTick >= ec.reconnectionMaxInterval {
				logger.Panic("failed to reconnect", zap.Error(err))
			} else {
				logger.Warn("could not reconnect, still trying", zap.Error(err))
			}
			return false, false
		}
		return true, false
	}, ec.reconnectionInitialInterval, ec.reconnectionMaxInterval+(ec.reconnectionInitialInterval))

	logger.Info("reconnected")
}

func (ec *Eth1Client) setLastFetchedBlock(block uint64) {
	ec.lastFetchedBlock.Store(block)
	ec.logger.Info("last fetched block", fields.BlockNumber(block))
	ec.metrics.LastFetchedBlock(block)
}
