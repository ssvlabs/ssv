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

const (
	// defaultHTTPConnectionTimeout is the default timeout for establishing HTTP connections.
	defaultHTTPConnectionTimeout = 10 * time.Second
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
	client   *ethclient.Client // HTTP client to execution node
	clientMu sync.RWMutex      // Protects client access
	logger   *zap.Logger

	httpAddr  string      // HTTP address of an execution client
	connected atomic.Bool // Connection state
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

	h.logger.Info("http log client connected", fields.Address(h.httpAddr))

	return nil
}

// ensureConnected ensures the client is connected, using lazy connection if needed.
// This method handles the automatic connection establishment for all HTTP operations.
func (h *HTTPLogClient) ensureConnected(ctx context.Context) error {
	h.clientMu.RLock()
	if h.connected.Load() && h.client != nil {
		h.clientMu.RUnlock()
		return nil
	}
	h.clientMu.RUnlock()

	return h.Connect(ctx, defaultHTTPConnectionTimeout)
}

// FetchLogs retrieves logs using eth_getLogs RPC over HTTP.
// Used as the first fallback when WebSocket hits read limits.
// Automatically establishes connection if not already connected.
func (h *HTTPLogClient) FetchLogs(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	if err := h.ensureConnected(ctx); err != nil {
		return nil, err
	}

	h.clientMu.RLock()
	client := h.client
	h.clientMu.RUnlock()

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
// Automatically establishes connection if not already connected.
func (h *HTTPLogClient) FetchLogsViaReceipts(ctx context.Context, contractAddr ethcommon.Address, blockNum uint64) ([]ethtypes.Log, error) {
	if err := h.ensureConnected(ctx); err != nil {
		return nil, err
	}

	h.clientMu.RLock()
	client := h.client
	h.clientMu.RUnlock()

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
