package executionclient

import (
	"context"
	"fmt"
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
//
// This is a block-level check only — it assumes all-or-nothing drops (an entire block's
// logs are missing). Partial drops (some logs present, some missing within the same block)
// have not been observed and are not handled here.
func (ec *ExecutionClient) verifyLogsWithBloom(ctx context.Context, logs []ethtypes.Log, fromBlock, toBlock uint64) ([]ethtypes.Log, error) {
	// Build a set of blocks that already have logs in the result.
	blocksWithLogs := make(map[uint64]bool)
	for i := range logs {
		blocksWithLogs[logs[i].BlockNumber] = true
	}

	recovered := false
	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		if blocksWithLogs[blockNum] {
			continue // block already has logs, nothing to verify
		}

		header, err := ec.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNum))
		if err != nil {
			return nil, fmt.Errorf("bloom check: fetch header for block %d: %w", blockNum, err)
		}

		if !header.Bloom.Test(ec.contractAddress.Bytes()) {
			continue // bloom says no SSV events in this block — genuine empty
		}

		// Bloom matched but FilterLogs returned nothing. This is inconclusive on its
		// own: it happens routinely as a benign bloom false positive (the filter is
		// probabilistic and other logs in the block can set the contract's bits), and
		// only occasionally because the EL actually dropped logs. We therefore log it
		// at debug and let the retry below decide — a successful recovery is what gets
		// promoted to a warning.
		ec.logger.Debug("bloom matched contract but EL returned no logs for block, retrying to confirm",
			fields.BlockNumber(blockNum),
			zap.String("contract", ec.contractAddress.Hex()),
		)

		blockLogs, err := ec.retryBlockLogs(ctx, blockNum)
		if err != nil {
			return nil, fmt.Errorf("bloom check: retry block %d logs: %w", blockNum, err)
		}

		if len(blockLogs) > 0 {
			// The retry recovered logs the EL had omitted from the original response.
			// The block bloom is consensus-verified, so these events are genuine and
			// were dropped by the EL — this is not a bloom false positive. This is the
			// actionable signal: the execution client returned incomplete logs, which
			// would have been silently skipped without this cross-check.
			ec.logger.Warn("recovered contract logs the execution client omitted on first request",
				fields.BlockNumber(blockNum),
				zap.Int("recovered_events", len(blockLogs)),
				zap.String("contract", ec.contractAddress.Hex()),
				zap.String("recommendation", "events were not lost, but the EL returned incomplete logs (e.g. known geth log-dropping bug) — consider updating or checking your execution client"),
			)
			recordBloomRecovery(ctx)
			logs = append(logs, blockLogs...)
			recovered = true
		} else {
			// Bloom false positive — address was in bloom but no actual logs for our contract.
			recordBloomFalsePositive(ctx)
		}
	}

	// Appending recovered logs above leaves the slice unsorted, so re-sort before returning.
	if recovered {
		sortLogsCanonical(logs)
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
			// Transient RPC failure the retry usually absorbs. The final attempt returns
			// this error, which the streaming retry loop logs at error level — so debug here.
			ec.logger.Debug("bloom retry: FilterLogs failed",
				fields.BlockNumber(blockNumber),
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			if attempt == DefaultBloomRetryAttempts {
				return nil, fmt.Errorf("bloom retry: filter logs for block %d: %w", blockNumber, err)
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
