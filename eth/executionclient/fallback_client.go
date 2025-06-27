package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=executionclient -destination=./fallback_client_mock.go -source=./fallback_client.go

// FallbackClientInterface defines the interface for fallback client operations.
type FallbackClientInterface interface {
	Connect(ctx context.Context, timeout time.Duration) error
	FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error)
	FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error)
	Close() error
}

var _ FallbackClientInterface = (*FallbackClient)(nil)

// FallbackClient provides HTTP-based fallback for log fetching.
type FallbackClient struct {
	client          *ethclient.Client
	clientMu        sync.RWMutex
	logger          *zap.Logger
	httpAddr        string
	connected       atomic.Bool
	lastConnectTime atomic.Int64
}

// NewFallbackClient creates a new FallbackClient instance.
func NewFallbackClient(addr string, logger *zap.Logger) FallbackClientInterface {
	if addr == "" {
		return nil
	}

	return &FallbackClient{
		httpAddr: normalizeHTTPAddr(addr),
		logger:   logger,
	}
}

// Connect establishes HTTP connection with retry logic.
func (f *FallbackClient) Connect(ctx context.Context, timeout time.Duration) error {
	f.clientMu.Lock()
	defer f.clientMu.Unlock()

	if f.connected.Load() && f.client != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := ethclient.DialContext(ctx, f.httpAddr)
	if err != nil {
		return fmt.Errorf("http fallback dial failed: %w", err)
	}

	f.client = client
	f.connected.Store(true)
	f.lastConnectTime.Store(time.Now().Unix())
	f.logger.Info("http fallback connected", fields.Address(f.httpAddr))

	return nil
}

// FetchLogs retrieves logs for a specific block using HTTP.
func (f *FallbackClient) FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	f.clientMu.RLock()
	client := f.client
	connected := f.connected.Load()
	f.clientMu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("http fallback not connected")
	}

	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{contractAddr},
		FromBlock: new(big.Int).SetUint64(blockNum),
		ToBlock:   new(big.Int).SetUint64(blockNum),
	}

	logs, err := client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("http filter logs failed: %w", err)
	}

	return logs, nil
}

// FetchLogsViaReceipts fetches logs by iterating through block transactions.
func (f *FallbackClient) FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	f.clientMu.RLock()
	client := f.client
	connected := f.connected.Load()
	f.clientMu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("http fallback not connected")
	}

	block, err := client.BlockByNumber(ctx, big.NewInt(int64(blockNum))) // nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block %d: %w", blockNum, err)
	}

	var logs []ethtypes.Log
	for _, tx := range block.Transactions() {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err != nil {
			f.logger.Debug("failed to fetch tx receipt",
				fields.TxHash(tx.Hash()),
				zap.Error(err))
			continue
		}

		for _, log := range receipt.Logs {
			if log.Address == contractAddr {
				logs = append(logs, *log)
			}
		}
	}

	return logs, nil
}

// Close closes the HTTP client connection.
func (f *FallbackClient) Close() error {
	f.clientMu.Lock()
	defer f.clientMu.Unlock()

	if f.client != nil {
		f.client.Close()
		f.client = nil
		f.connected.Store(false)
	}

	return nil
}

// normalizeHTTPAddr converts WebSocket URLs to HTTP equivalents.
func normalizeHTTPAddr(addr string) string {
	addr = strings.Replace(addr, "ws://", "http://", 1)
	addr = strings.Replace(addr, "wss://", "https://", 1)
	return addr
}
