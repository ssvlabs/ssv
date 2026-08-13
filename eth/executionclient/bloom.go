package executionclient

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// verifyLogCompleteness cross-checks the logs returned by FilterLogs against each block's
// bloom filter and recovers events the EL silently omitted. Without it, an incomplete
// eth_getLogs response would permanently skip the affected blocks' events.
//
// eth_getLogs is served from the EL's derived log index, which — unlike block headers and
// receipts — is not consensus-verified and is not guaranteed complete relative to a block's
// actual logs. Storage checksums protect byte-integrity, not completeness: an index that is
// still building (snap sync), pruned, mid-rebuild, or buggy can return fewer logs than a
// block holds, with no error (e.g. the geth log-index bug in #2722). This is defense-in-depth
// from any EL/version — it anchors completeness to consensus-committed data (header blooms,
// which have no false negatives, plus receipts) instead of trusting that index.
//
// It runs in stages, cheapest first:
//
//  1. Fetch the headers of all blocks that returned no logs (one batched request) and test
//     each bloom against the contract address. Blooms are consensus-verified and have no
//     false negatives, so a non-matching bloom proves the block is genuinely empty.
//  2. Re-request each remaining suspect block with a single-block query (all sent as one
//     batched request). This recovers intermittent per-request drops (observed with geth).
//  3. Resolve suspects that still return nothing via eth_getBlockReceipts. Receipts come
//     from the EL's receipt store rather than its log index, so they settle the ambiguity
//     decisively: either they contain the events a broken log index will never return
//     (recovered directly from the receipts), or they prove the bloom hit was a false
//     positive. ELs without eth_getBlockReceipts fall back to timed single-block retries.
//
// This is the inline verification path (streaming, and the resync-repair). Optimistic
// historical sync skips it and is checked afterwards by the background verifier, which reuses
// the same recovery via VerifyLogs.
//
// This is a block-level check only — it assumes all-or-nothing drops (an entire block's
// logs are missing). Partial drops (some logs present, some missing within the same block)
// have not been observed and are not handled here.
func (ec *ExecutionClient) verifyLogCompleteness(ctx context.Context, logs []ethtypes.Log, fromBlock, toBlock uint64) ([]ethtypes.Log, error) {
	// Build a set of blocks that already have logs in the result.
	blocksWithLogs := make(map[uint64]bool)
	for i := range logs {
		blocksWithLogs[logs[i].BlockNumber] = true
	}

	emptyBlocks := make([]uint64, 0, toBlock-fromBlock+1)
	for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
		if !blocksWithLogs[blockNum] {
			emptyBlocks = append(emptyBlocks, blockNum)
		}
	}
	if len(emptyBlocks) == 0 {
		return logs, nil
	}

	start := time.Now()
	headers, err := ec.client.HeadersByNumbers(ctx, emptyBlocks)
	recordRequest(ctx, ec.logger, "HeadersByNumbers", ec, time.Since(start), err)
	if err != nil {
		return nil, fmt.Errorf("completeness check: fetch headers for blocks %d-%d: %w", fromBlock, toBlock, err)
	}

	suspects := make([]uint64, 0, len(emptyBlocks))
	for i, header := range headers {
		if header.Bloom.Test(ec.contractAddress.Bytes()) {
			suspects = append(suspects, emptyBlocks[i])
		}
	}
	if len(suspects) == 0 {
		return logs, nil
	}

	// Bloom matched but FilterLogs returned nothing. This is inconclusive on its own: it
	// happens routinely as a benign bloom false positive (the filter is probabilistic and
	// other logs in the block can set the contract's bits), and only occasionally because
	// the EL actually dropped logs. We therefore log it at debug and let the stages below
	// decide — a successful recovery is what gets promoted to a warning.
	ec.logger.Debug("bloom matched contract for blocks the EL returned no logs for, re-requesting to confirm",
		zap.Uint64s("blocks", suspects),
		zap.String("contract", ec.contractAddress.Hex()),
	)

	start = time.Now()
	suspectLogs, err := ec.client.SingleBlockLogs(ctx, ec.contractAddress, suspects)
	recordRequest(ctx, ec.logger, "SingleBlockLogs", ec, time.Since(start), err)
	if err != nil {
		return nil, fmt.Errorf("completeness check: re-request logs for suspect blocks: %w", err)
	}

	recovered := false
	for i, blockLogs := range suspectLogs {
		if len(blockLogs) == 0 {
			blockLogs, err = ec.resolveSuspectBlock(ctx, suspects[i])
			if err != nil {
				return nil, err
			}
			if len(blockLogs) == 0 {
				continue
			}
		} else {
			ec.reportOmittedLogsRecovery(ctx, suspects[i], len(blockLogs))
		}
		logs = append(logs, blockLogs...)
		recovered = true
	}

	// Re-sort if we appended recovered logs so downstream receives them in block/tx order.
	// Match PackLogs' (block, tx) key exactly — it re-sorts afterwards, so any finer tiebreaker
	// here would just be undone.
	if recovered {
		sort.Slice(logs, func(i, j int) bool {
			if logs[i].BlockNumber != logs[j].BlockNumber {
				return logs[i].BlockNumber < logs[j].BlockNumber
			}
			return logs[i].TxIndex < logs[j].TxIndex
		})
	}

	return logs, nil
}

// resolveSuspectBlock settles a block whose bloom matches the contract but for which the
// EL keeps returning no logs: either the bloom hit is a false positive, or the EL's log
// index is persistently broken for that block. eth_getBlockReceipts distinguishes the two
// (and directly yields the missing logs when it is the latter); ELs without the method fall
// back to timed retries, which can only recover intermittent drops. Returns the recovered
// logs, or nil when the block is presumed a false positive.
func (ec *ExecutionClient) resolveSuspectBlock(ctx context.Context, blockNum uint64) ([]ethtypes.Log, error) {
	if !ec.receiptsUnsupported.Load() {
		blockLogs, err := ec.blockLogsFromReceipts(ctx, blockNum)
		if err == nil {
			if len(blockLogs) > 0 {
				// The log index keeps omitting events that the block's receipts prove exist.
				// Unlike a transient drop, this cannot be fixed by retrying eth_getLogs — any
				// range query over this block is silently incomplete.
				ec.logger.Warn("recovered contract logs from block receipts that the execution client's log index did not return",
					fields.BlockNumber(blockNum),
					zap.Int("recovered_events", len(blockLogs)),
					zap.String("contract", ec.contractAddress.Hex()),
					zap.String("recommendation", "the EL omits events from eth_getLogs responses that its own receipts contain — its log index may be incomplete or corrupt, consider resyncing or updating your execution client"),
				)
				recordBloomReceiptsRecovery(ctx)
			} else {
				// Receipts confirm the block holds no contract events — bloom false positive.
				recordBloomFalsePositive(ctx)
			}
			return blockLogs, nil
		}
		if !isRPCMethodNotFoundError(err) {
			return nil, fmt.Errorf("completeness check: fetch receipts for block %d: %w", blockNum, err)
		}
		ec.receiptsUnsupported.Store(true)
		ec.logger.Info("execution client does not support eth_getBlockReceipts, falling back to timed log re-requests for suspect blocks")
	}

	blockLogs, err := ec.retryBlockLogs(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("completeness check: retry block %d logs: %w", blockNum, err)
	}
	if len(blockLogs) > 0 {
		ec.reportOmittedLogsRecovery(ctx, blockNum, len(blockLogs))
	} else {
		// Presumed bloom false positive — the address was in the bloom, but the EL never
		// produced logs for it and receipts were unavailable to prove it either way.
		recordBloomFalsePositive(ctx)
	}
	return blockLogs, nil
}

// reportOmittedLogsRecovery logs and counts the recovery of logs the EL omitted from an
// eth_getLogs response but returned on a re-request (an intermittent drop). The block
// bloom is consensus-verified, so the recovered events are genuine — the EL returned an
// incomplete response that would have been silently accepted without the cross-check.
func (ec *ExecutionClient) reportOmittedLogsRecovery(ctx context.Context, blockNum uint64, recoveredEvents int) {
	ec.logger.Warn("recovered contract logs the execution client omitted on first request",
		fields.BlockNumber(blockNum),
		zap.Int("recovered_events", recoveredEvents),
		zap.String("contract", ec.contractAddress.Hex()),
		zap.String("recommendation", "events were not lost, but the EL returned incomplete logs (e.g. known geth log-dropping bug) — consider updating or checking your execution client"),
	)
	recordBloomRecovery(ctx)
}

// VerifyLogs fetches the contract's logs for [fromBlock, toBlock] and returns them verified
// complete — running the exact same bloom/receipts completeness check as the inline path, so
// only genuinely-omitted logs trigger receipts recovery (and the accurate recovery logging
// and metrics). It is the background verifier's per-chunk operation for a range that was
// synced optimistically. Returned BlockLogs are complete and ordered.
func (ec *ExecutionClient) VerifyLogs(ctx context.Context, fromBlock, toBlock uint64) ([]BlockLogs, error) {
	if fromBlock > toBlock {
		return nil, ErrBadInput
	}

	logs, err := ec.subdivideLogFetch(ctx, ethereum.FilterQuery{
		Addresses: []ethcommon.Address{ec.contractAddress},
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
	})
	if err != nil {
		return nil, fmt.Errorf("verify logs [%d,%d]: fetch: %w", fromBlock, toBlock, err)
	}

	logs, err = ec.verifyLogCompleteness(ctx, logs, fromBlock, toBlock)
	if err != nil {
		return nil, fmt.Errorf("verify logs [%d,%d]: completeness check: %w", fromBlock, toBlock, err)
	}

	validLogs := make([]ethtypes.Log, 0, len(logs))
	for _, log := range logs {
		if log.Removed {
			continue // reorg'd log; not expected below the follow distance, skip defensively
		}
		validLogs = append(validLogs, log)
	}

	return PackLogs(validLogs), nil
}

// blockLogsFromReceipts fetches the given block's transaction receipts and extracts the
// logs emitted by the contract.
func (ec *ExecutionClient) blockLogsFromReceipts(ctx context.Context, blockNum uint64) ([]ethtypes.Log, error) {
	start := time.Now()
	receipts, err := ec.client.BlockReceipts(ctx, blockNum)
	recordRequest(ctx, ec.logger, "BlockReceipts", ec, time.Since(start), err)
	if err != nil {
		return nil, err
	}

	var logs []ethtypes.Log
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		for _, log := range receipt.Logs {
			if log != nil && log.Address == ec.contractAddress {
				logs = append(logs, *log)
			}
		}
	}
	return logs, nil
}

// BlockContractLogs returns the contract's logs for a block derived from the block's receipts —
// the index-independent source of truth, so it's authoritative regardless of the EL's log index.
// receiptsAvailable is false when the EL doesn't support eth_getBlockReceipts (remembered so it
// isn't re-attempted), in which case the caller has no authoritative source for the block.
func (ec *ExecutionClient) BlockContractLogs(ctx context.Context, block uint64) ([]ethtypes.Log, bool, error) {
	if ec.receiptsUnsupported.Load() {
		return nil, false, nil
	}
	logs, err := ec.blockLogsFromReceipts(ctx, block)
	if err != nil {
		if isRPCMethodNotFoundError(err) {
			ec.receiptsUnsupported.Store(true)
			ec.logger.Info("execution client does not support eth_getBlockReceipts; background verification can't authoritatively check blocks on it")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("fetch receipts for block %d: %w", block, err)
	}
	return logs, true, nil
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
			// Transient RPC failure the retry usually absorbs; only the final attempt's
			// error propagates to the caller, which logs it — so debug here.
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
