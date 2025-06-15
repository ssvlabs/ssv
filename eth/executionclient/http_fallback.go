package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

type HTTPFallback struct {
	client    *ethclient.Client
	clientMu  sync.RWMutex
	addr      string
	logger    *zap.Logger
	connected bool
}

// NewHTTPFallback creates a new HTTP fallback handler.
func NewHTTPFallback(addr string, logger *zap.Logger) *HTTPFallback {
	if addr == "" {
		return nil
	}

	return &HTTPFallback{
		addr:   normalizeHTTPAddr(addr),
		logger: logger,
	}
}

// Connect establishes HTTP connection.
func (h *HTTPFallback) Connect(ctx context.Context, timeout time.Duration) error {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	if h.connected && h.client != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := ethclient.DialContext(ctx, h.addr)
	if err != nil {
		return fmt.Errorf("http fallback dial failed: %w", err)
	}

	h.client = client
	h.connected = true
	h.logger.Info("http fallback connected", fields.Address(h.addr))

	return nil
}

// FetchLogs retrieves logs for a specific block using HTTP.
func (h *HTTPFallback) FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	h.clientMu.RLock()
	client := h.client
	h.clientMu.RUnlock()

	if client == nil {
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
func (h *HTTPFallback) FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	h.clientMu.RLock()
	client := h.client
	h.clientMu.RUnlock()

	if client == nil {
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
			h.logger.Debug("failed to fetch tx receipt",
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
func (h *HTTPFallback) Close() error {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	if h.client != nil {
		h.client.Close()
		h.client = nil
		h.connected = false
	}

	return nil
}

// normalizeHTTPAddr normalizes a given WebSocket or HTTPS address to its HTTP equivalent.
func normalizeHTTPAddr(addr string) string {
	addr = strings.Replace(addr, "ws://", "http://", 1)
	addr = strings.Replace(addr, "wss://", "https://", 1)
	return addr
}
