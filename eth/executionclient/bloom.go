package executionclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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
//  3. Resolve suspects that still return nothing via receipts: the client's own, else another
//     client's (MultiClient). Receipts come from the EL's receipt store rather than its log
//     index, so they settle the ambiguity decisively: either they contain the events a broken
//     log index will never return (recovered directly from the receipts), or they prove the
//     bloom hit was a false positive.
//  4. Without receipts, re-request the remaining suspects on a timed schedule (one batched
//     request per attempt), which can only recover intermittent drops.
//
// Suspects none of this settles are returned as unsettled rather than assumed empty: without
// receipts, a bloom false positive and a persistently dropped block look the same, so the caller
// must record them for later verification.
//
// This is the inline verification path (streaming, restart catch-ups and the resync-repair), and
// the background verifier reuses it via VerifyLogs.
//
// This is a block-level check only — it assumes all-or-nothing drops (an entire block's
// logs are missing). Partial drops (some logs present, some missing within the same block)
// have not been observed and are not handled here.
func (ec *ExecutionClient) verifyLogCompleteness(ctx context.Context, logs []ethtypes.Log, fromBlock, toBlock uint64) (_ []ethtypes.Log, unsettled []uint64, err error) {
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
		return logs, nil, nil
	}

	start := time.Now()
	headers, err := ec.client.HeadersByNumbers(ctx, emptyBlocks)
	recordRequest(ctx, ec.logger, "HeadersByNumbers", ec, time.Since(start), err)
	if err != nil {
		return nil, nil, fmt.Errorf("completeness check: fetch headers for blocks %d-%d: %w", fromBlock, toBlock, err)
	}

	suspects := make([]uint64, 0, len(emptyBlocks))
	for i, header := range headers {
		if header.Bloom.Test(ec.contractAddress.Bytes()) {
			suspects = append(suspects, emptyBlocks[i])
		}
	}
	if len(suspects) == 0 {
		return logs, nil, nil
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
		return nil, nil, fmt.Errorf("completeness check: re-request logs for suspect blocks: %w", err)
	}

	recovered := false
	var noReceipts []uint64
	for i, blockLogs := range suspectLogs {
		block := suspects[i]
		if len(blockLogs) > 0 {
			ec.reportOmittedLogsRecovery(ctx, block, len(blockLogs))
			logs = append(logs, blockLogs...)
			recovered = true
			continue
		}

		receiptLogs, settled, err := ec.settleSuspect(ctx, block)
		if err != nil {
			return nil, nil, fmt.Errorf("completeness check: fetch receipts for block %d: %w", block, err)
		}
		switch {
		case !settled:
			noReceipts = append(noReceipts, block)
		case len(receiptLogs) > 0:
			// The log index keeps omitting events that the block's receipts prove exist.
			// Unlike a transient drop, this cannot be fixed by retrying eth_getLogs — any
			// range query over this block is silently incomplete.
			ec.logger.Warn("recovered contract logs from block receipts that the execution client's log index did not return",
				fields.BlockNumber(block),
				zap.Int("recovered_events", len(receiptLogs)),
				zap.String("contract", ec.contractAddress.Hex()),
				zap.String("recommendation", "the EL omits events from eth_getLogs responses that its own receipts contain — its log index may be incomplete or corrupt, consider resyncing or updating your execution client"),
			)
			recordBloomReceiptsRecovery(ctx)
			logs = append(logs, receiptLogs...)
			recovered = true
		default:
			// Receipts confirm the block holds no contract events — bloom false positive.
			recordBloomFalsePositive(ctx)
		}
	}

	if len(noReceipts) > 0 {
		retried, stillEmpty, err := ec.retrySuspects(ctx, noReceipts)
		if err != nil {
			return nil, nil, fmt.Errorf("completeness check: retry suspect blocks: %w", err)
		}
		for _, bl := range retried {
			ec.reportOmittedLogsRecovery(ctx, bl.BlockNumber, len(bl.Logs))
			logs = append(logs, bl.Logs...)
			recovered = true
		}
		unsettled = stillEmpty
	}
	if len(unsettled) > 0 {
		recordBloomUnsettled(ctx, len(unsettled))
		ec.logger.Warn("no authoritative source for blocks whose bloom matches the contract but that returned no logs; leaving them unsettled",
			zap.Uint64s("blocks", unsettled),
			zap.String("contract", ec.contractAddress.Hex()),
			zap.String("recommendation", "no execution client could serve these blocks' receipts (unsupported, pruned or still syncing), so a dropped event there can't be ruled out until one can — use an execution client that serves eth_getBlockReceipts"),
		)
	}

	// Appending recovered logs above leaves the slice unsorted, so re-sort before returning.
	if recovered {
		sortLogsCanonical(logs)
	}

	return logs, unsettled, nil
}

// settleSuspect resolves a suspect block against receipts: the client's own, else another
// client's (MultiClient). settled is false when no client could serve them.
func (ec *ExecutionClient) settleSuspect(ctx context.Context, block uint64) (logs []ethtypes.Log, settled bool, err error) {
	logs, settled, err = ec.BlockContractLogs(ctx, block)
	if err != nil || settled || ec.receiptsFallback == nil {
		return logs, settled, err
	}

	logs, settled, err = ec.receiptsFallback(ctx, block)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// Other clients failing leaves the block unsettled; it isn't this client's fetch failing.
		ec.logger.Debug("other clients couldn't serve receipts for a suspect block",
			fields.BlockNumber(block), zap.Error(err))
		return nil, false, nil
	}
	return logs, settled, nil
}

// retrySuspects re-requests suspect blocks' logs on the timed-retry schedule, all of them in one
// batched request per attempt (the re-request that left them suspect counts as the first). It
// returns the blocks that turned up logs, and those still empty.
func (ec *ExecutionClient) retrySuspects(ctx context.Context, blocks []uint64) (recovered []BlockLogs, empty []uint64, err error) {
	for attempt := 2; attempt <= DefaultBloomRetryAttempts && len(blocks) > 0; attempt++ {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ec.closed:
			return nil, nil, ErrClosed
		case <-time.After(DefaultBloomRetryDelay):
		}

		start := time.Now()
		perBlock, err := ec.client.SingleBlockLogs(ctx, ec.contractAddress, blocks)
		recordRequest(ctx, ec.logger, "SingleBlockLogs", ec, time.Since(start), err)
		if err != nil {
			if attempt == DefaultBloomRetryAttempts {
				return nil, nil, err
			}
			// Transient RPC failure the next attempt usually absorbs.
			ec.logger.Debug("re-request of suspect blocks failed; retrying",
				zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		remaining := make([]uint64, 0, len(blocks))
		for i, blockLogs := range perBlock {
			if len(blockLogs) > 0 {
				recovered = append(recovered, BlockLogs{BlockNumber: blocks[i], Logs: blockLogs})
			} else {
				remaining = append(remaining, blocks[i])
			}
		}
		blocks = remaining
	}
	return recovered, blocks, nil
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
// synced optimistically. Returned BlockLogs are ordered, with a log-less Unsettled entry for each
// block the check couldn't settle.
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

	logs, unsettled, err := ec.verifyLogCompleteness(ctx, logs, fromBlock, toBlock)
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

	return packWithUnsettled(validLogs, unsettled), nil
}

// receiptsUnsupportedRecheckInterval is how long the EL's rejection of eth_getBlockReceipts is
// remembered before the method is probed again.
const receiptsUnsupportedRecheckInterval = time.Hour

// receiptsKnownUnsupported reports whether the EL rejected eth_getBlockReceipts within the last
// receiptsUnsupportedRecheckInterval.
func (ec *ExecutionClient) receiptsKnownUnsupported() bool {
	at := ec.receiptsUnsupportedAt.Load()
	return at != 0 && time.Since(time.Unix(at, 0)) < receiptsUnsupportedRecheckInterval
}

// rememberReceiptsUnsupported records that the EL just rejected eth_getBlockReceipts.
func (ec *ExecutionClient) rememberReceiptsUnsupported() {
	ec.receiptsUnsupportedAt.Store(time.Now().Unix())
}

// errInconsistentEmptyReceipts marks a block that returned zero receipts even though every
// caller only consults receipts for a block already evidenced to hold transactions (a bloom hit,
// a recorded/observed log, or a non-empty transaction list). Zero receipts is then a
// self-inconsistent response — a broken receipt store, not an authoritatively-empty block.
var errInconsistentEmptyReceipts = errors.New("block returned zero receipts but is known to have transactions")

// blockLogsFromReceipts fetches the given block's transaction receipts and extracts the logs
// emitted by the contract. A block with no receipts at all yields errInconsistentEmptyReceipts.
func (ec *ExecutionClient) blockLogsFromReceipts(ctx context.Context, blockNum uint64) ([]ethtypes.Log, error) {
	start := time.Now()
	receipts, err := ec.client.BlockReceipts(ctx, blockNum)
	recordRequest(ctx, ec.logger, "BlockReceipts", ec, time.Since(start), err)
	if err != nil {
		return nil, err
	}
	if len(receipts) == 0 {
		return nil, errInconsistentEmptyReceipts
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
// receiptsAvailable is false when the EL can't authoritatively serve the block: it doesn't
// support eth_getBlockReceipts (remembered for receiptsUnsupportedRecheckInterval), or it has no
// usable receipts for the block (none at all, or not found).
func (ec *ExecutionClient) BlockContractLogs(ctx context.Context, block uint64) ([]ethtypes.Log, bool, error) {
	if ec.receiptsKnownUnsupported() {
		return nil, false, nil
	}
	logs, err := ec.blockLogsFromReceipts(ctx, block)
	switch {
	case err == nil:
		return logs, true, nil
	case isRPCMethodNotFoundError(err):
		ec.rememberReceiptsUnsupported()
		ec.logger.Info("execution client does not support eth_getBlockReceipts, so it can't authoritatively verify blocks whose bloom matches the contract",
			zap.Duration("recheck_in", receiptsUnsupportedRecheckInterval))
		return nil, false, nil
	case errors.Is(err, errInconsistentEmptyReceipts), errors.Is(err, ethereum.NotFound):
		// No authoritative source: report unavailable rather than read a broken receipt store as
		// an empty block. Callers report what stays unsettled, so the detail is debug.
		ec.logger.Debug("no usable receipts for a block expected to hold transactions",
			fields.BlockNumber(block), zap.Error(err))
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("fetch receipts for block %d: %w", block, err)
	}
}
