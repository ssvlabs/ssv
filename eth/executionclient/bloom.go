package executionclient

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// verifyLogsWithBloom cross-checks the logs returned by FilterLogs against each block's
// bloom filter. Blocks that returned no logs but whose bloom filter matches the contract
// address are retried individually to recover potentially dropped events (a known Geth bug).
func (ec *ExecutionClient) verifyLogsWithBloom(ctx context.Context, logs []ethtypes.Log, fromBlock, toBlock uint64) ([]ethtypes.Log, error) {
	// Build a set of blocks that already have logs in the result.
	blocksWithLogs := make(map[uint64]bool)
	for i := range logs {
		blocksWithLogs[logs[i].BlockNumber] = true
	}

	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		if blocksWithLogs[blockNum] {
			continue // block already has logs, nothing to verify
		}

		header, err := ec.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNum))
		if err != nil {
			return nil, err
		}

		if !header.Bloom.Test(ec.contractAddress.Bytes()) {
			continue // bloom says no SSV events in this block — genuine empty
		}

		// Bloom matched but FilterLogs returned nothing — suspected EL bug.
		ec.logger.Warn("bloom filter mismatch: block bloom matches contract but no logs returned, retrying",
			fields.BlockNumber(blockNum),
			zap.String("contract", ec.contractAddress.Hex()),
		)

		recovered, err := ec.retryBlockLogs(ctx, blockNum)
		if err != nil {
			return nil, err
		}

		if len(recovered) > 0 {
			ec.logger.Warn("bloom cross-check recovered missing events",
				fields.BlockNumber(blockNum),
				zap.Int("recovered_events", len(recovered)),
			)
			recordBloomRecovery(ctx)
			logs = append(logs, recovered...)
		} else {
			// Bloom false positive — address was in bloom but no actual logs for our contract.
			recordBloomFalsePositive(ctx)
		}
	}

	return logs, nil
}

// retryBlockLogs retries FilterLogs for a single block up to DefaultBloomRetryAttempts times
// with DefaultBloomRetryDelay between attempts. Returns recovered logs, nil (false positive),
// or an error on RPC failure.
func (ec *ExecutionClient) retryBlockLogs(ctx context.Context, blockNumber uint64) ([]ethtypes.Log, error) {
	query := ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ec.contractAddress},
		FromBlock: new(big.Int).SetUint64(blockNumber),
		ToBlock:   new(big.Int).SetUint64(blockNumber),
	}

	for attempt := 1; attempt <= DefaultBloomRetryAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ec.closed:
				return nil, ErrClosed
			case <-time.After(DefaultBloomRetryDelay):
			}
		}

		logs, err := ec.FilterLogs(ctx, query)
		if err != nil {
			ec.logger.Warn("bloom retry: FilterLogs failed",
				fields.BlockNumber(blockNumber),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt == DefaultBloomRetryAttempts {
				return nil, err
			}
			continue
		}

		if len(logs) > 0 {
			return logs, nil
		}
	}

	// All retries returned 0 logs — bloom false positive.
	return nil, nil
}
