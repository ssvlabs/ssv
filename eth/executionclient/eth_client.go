package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// maxRPCBatchSize bounds how many elements a single batched RPC request carries. Verification
// chunks can be thousands of blocks wide, but many hosted providers cap JSON-RPC batches
// (commonly around 100), so keeping each batch small means an over-limit request degrades to a
// few smaller batches rather than one wholesale rejection.
const maxRPCBatchSize = 100

// ethClient is a wrapper around ethclient.Client to add a default timeout for every call we
// make with ethclient.Client. This allows us to manage timeouts for ethclient.Client in one
// place such that we don't accidentally forget to set a timeout for some ethclient.Client call.
type ethClient struct {
	client *ethclient.Client
	logger *zap.Logger

	// reqTimeout specifies the default timeout to be used with every ethclient.Client call.
	reqTimeout time.Duration

	// batchingUnsupported remembers that a batched request failed wholesale (yet the sequential
	// fallback succeeded), i.e. the provider rejects batching, so subsequent calls skip the
	// doomed batch and go straight to sequential requests.
	batchingUnsupported atomic.Bool
}

func newEthClient(client *ethclient.Client, reqTimeout time.Duration, logger *zap.Logger) *ethClient {
	return &ethClient{
		client:     client,
		logger:     logger,
		reqTimeout: reqTimeout,
	}
}

func (c *ethClient) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SyncProgress(reqCtx)
}

func (c *ethClient) BlockNumber(ctx context.Context) (uint64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.BlockNumber(reqCtx)
}

func (c *ethClient) HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.HeaderByNumber(reqCtx, blockNumber)
}

func (c *ethClient) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SubscribeFilterLogs(reqCtx, q, ch)
}

func (c *ethClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.FilterLogs(reqCtx, q)
}

// HeadersByNumbers fetches the headers for the given block numbers with a single batched
// eth_getBlockByNumber request. Elements that the batch could not serve (some RPC servers
// restrict or disable batching, wholesale or per element) are recovered with sequential
// fetches. Returned headers are ordered to match blockNumbers.
func (c *ethClient) HeadersByNumbers(ctx context.Context, blockNumbers []uint64) ([]*ethtypes.Header, error) {
	if len(blockNumbers) == 0 {
		return nil, nil
	}

	headers := make([]*ethtypes.Header, len(blockNumbers))
	// Split into bounded windows so a wide request stays within providers' batch limits.
	for start := 0; start < len(blockNumbers); start += maxRPCBatchSize {
		end := min(start+maxRPCBatchSize, len(blockNumbers))
		window := blockNumbers[start:end]

		attemptedBatch := !c.batchingUnsupported.Load()
		var batch []rpc.BatchElem
		var batchErr error
		if attemptedBatch {
			batch = make([]rpc.BatchElem, len(window))
			for i, blockNumber := range window {
				batch[i] = rpc.BatchElem{
					Method: "eth_getBlockByNumber",
					Args:   []any{hexutil.EncodeUint64(blockNumber), false},
					Result: &headers[start+i],
				}
			}

			reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
			batchErr = c.client.Client().BatchCallContext(reqCtx, batch)
			cancel()
		}

		for i, blockNumber := range window {
			idx := start + i
			if attemptedBatch && batchErr == nil && batch[i].Error == nil && headers[idx] != nil {
				continue
			}
			header, err := c.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
			if err != nil {
				return nil, fmt.Errorf("get header for block %d: %w", blockNumber, err)
			}
			headers[idx] = header
		}

		c.rememberBatchingUnsupported(attemptedBatch, batchErr)
	}

	return headers, nil
}

// rememberBatchingUnsupported latches batchingUnsupported when a batch failed wholesale yet the
// sequential fallback (run just above) recovered — distinguishing a provider that rejects batching
// from a general outage, where the sequential calls would have failed too. Timeouts and
// cancellations are excluded: they mean slow or interrupted, not unsupported, so a batch-only
// timeout can't wrongly pin a batching-capable provider to sequential. One-way, reset on reconnect.
func (c *ethClient) rememberBatchingUnsupported(attemptedBatch bool, batchErr error) {
	if !attemptedBatch || batchErr == nil {
		return
	}
	if errors.Is(batchErr, context.DeadlineExceeded) || errors.Is(batchErr, context.Canceled) {
		return
	}
	if c.batchingUnsupported.CompareAndSwap(false, true) {
		c.logger.Warn("execution client rejected a batched request; falling back to sequential requests for the rest of this connection",
			zap.Error(batchErr))
	}
}

// SingleBlockLogs fetches, for each of the given blocks, the logs emitted by the given
// address, using a single batched eth_getLogs request with one single-block query per
// block. Elements the batch could not serve are recovered with sequential FilterLogs
// calls. Returned log slices are ordered to match blockNumbers.
func (c *ethClient) SingleBlockLogs(ctx context.Context, address ethcommon.Address, blockNumbers []uint64) ([][]ethtypes.Log, error) {
	if len(blockNumbers) == 0 {
		return nil, nil
	}

	blockLogs := make([][]ethtypes.Log, len(blockNumbers))
	// Split into bounded windows so a wide request stays within providers' batch limits.
	for start := 0; start < len(blockNumbers); start += maxRPCBatchSize {
		end := min(start+maxRPCBatchSize, len(blockNumbers))
		window := blockNumbers[start:end]

		attemptedBatch := !c.batchingUnsupported.Load()
		var batch []rpc.BatchElem
		var batchErr error
		if attemptedBatch {
			batch = make([]rpc.BatchElem, len(window))
			for i, blockNumber := range window {
				batch[i] = rpc.BatchElem{
					Method: "eth_getLogs",
					Args: []any{map[string]any{
						"address":   address,
						"fromBlock": hexutil.EncodeUint64(blockNumber),
						"toBlock":   hexutil.EncodeUint64(blockNumber),
					}},
					Result: &blockLogs[start+i],
				}
			}

			reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
			batchErr = c.client.Client().BatchCallContext(reqCtx, batch)
			cancel()
		}

		for i, blockNumber := range window {
			idx := start + i
			if attemptedBatch && batchErr == nil && batch[i].Error == nil {
				continue
			}
			logs, err := c.FilterLogs(ctx, ethereum.FilterQuery{
				Addresses: []ethcommon.Address{address},
				FromBlock: new(big.Int).SetUint64(blockNumber),
				ToBlock:   new(big.Int).SetUint64(blockNumber),
			})
			if err != nil {
				return nil, fmt.Errorf("get logs for block %d: %w", blockNumber, err)
			}
			blockLogs[idx] = logs
		}

		c.rememberBatchingUnsupported(attemptedBatch, batchErr)
	}

	return blockLogs, nil
}

// BlockReceipts fetches all transaction receipts of the given block via eth_getBlockReceipts.
func (c *ethClient) BlockReceipts(ctx context.Context, blockNumber uint64) ([]*ethtypes.Receipt, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	// #nosec G115 -- block numbers fit in int64
	return c.client.BlockReceipts(reqCtx, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNumber)))
}

func (c *ethClient) SubscribeNewHead(ctx context.Context, heads chan *ethtypes.Header) (ethereum.Subscription, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.SubscribeNewHead(reqCtx, heads)
}

func (c *ethClient) ChainID(ctx context.Context) (*big.Int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	return c.client.ChainID(reqCtx)
}

func (c *ethClient) Close() {
	c.client.Close()
}
