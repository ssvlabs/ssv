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

//go:generate go tool -modfile=../../tool.mod mockgen -package=executionclient -destination=./http_log_client_mock.go -source=./http_log_client.go

// HTTPLogClientInterface provides HTTP-based log fetching when WebSocket connections fail.
//
// When fetching logs from Ethereum nodes, WebSocket connections can hit read limits
// or query size limits, especially for blocks with many transactions. This interface
// provides HTTP-based methods to retrieve logs as an alternative to WebSocket transport.
type HTTPLogClientInterface interface {
	// Connect establishes HTTP connection to the execution client.
	Connect(ctx context.Context, timeout time.Duration) error

	// FetchLogs retrieves logs using eth_getLogs over HTTP instead of WebSocket.
	FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error)

	// FetchLogsViaReceipts gets logs by fetching individual transaction receipts.
	// Used when eth_getLogs fails due to too many logs in the block.
	// Slower but works around query size limits.
	FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error)

	// Close closes the HTTP connection.
	Close() error
}

var _ HTTPLogClientInterface = (*HTTPLogClient)(nil)

// HTTPLogClient provides HTTP-based log fetching as backup when WebSocket fails.
type HTTPLogClient struct {
	client          *ethclient.Client // HTTP client to execution node
	clientMu        sync.RWMutex      // Protects client access
	logger          *zap.Logger
	httpAddr        string      // HTTP address of execution client
	connected       atomic.Bool // Connection state
	lastConnectTime atomic.Int64
}

// NewHTTPLogClient creates an HTTP log client for the given execution client address.
// Converts WebSocket URLs (ws://, wss://) to HTTP URLs automatically.
// Returns nil if addr is empty (no HTTP log client configured).
func NewHTTPLogClient(addr string, logger *zap.Logger) HTTPLogClientInterface {
	if addr == "" {
		return nil
	}

	return &HTTPLogClient{
		httpAddr: normalizeHTTPAddr(addr),
		logger:   logger,
	}
}

// Connect establishes HTTP connection to the execution client.
// Returns immediately if already connected.
func (h *HTTPLogClient) Connect(ctx context.Context, timeout time.Duration) error {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	if h.connected.Load() && h.client != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := ethclient.DialContext(ctx, h.httpAddr)
	if err != nil {
		return fmt.Errorf("http log client dial failed: %w", err)
	}

	h.client = client
	h.connected.Store(true)
	h.lastConnectTime.Store(time.Now().Unix())
	h.logger.Info("http log client connected", fields.Address(h.httpAddr))

	return nil
}

// FetchLogs retrieves logs using eth_getLogs RPC over HTTP.
// Used as first fallback when WebSocket hits read limits.
func (h *HTTPLogClient) FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	h.clientMu.RLock()
	client := h.client
	connected := h.connected.Load()
	h.clientMu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("http log client not connected")
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

// FetchLogsViaReceipts gets logs by fetching individual transaction receipts.
// Used when eth_getLogs fails due to too many logs in the block.
// Slower but works around query size limits.
func (h *HTTPLogClient) FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	h.clientMu.RLock()
	client := h.client
	connected := h.connected.Load()
	h.clientMu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("http log client not connected")
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
func (h *HTTPLogClient) Close() error {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()

	if h.connected.Load() {
		h.client.Close()
		h.client = nil
		h.connected.Store(false)
	}

	return nil
}

// normalizeHTTPAddr converts WebSocket URLs to HTTP equivalents.
func normalizeHTTPAddr(addr string) string {
	addr = strings.Replace(addr, "ws://", "http://", 1)
	addr = strings.Replace(addr, "wss://", "https://", 1)
	return addr
}
