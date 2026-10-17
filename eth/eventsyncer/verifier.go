package eventsyncer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/observability/log/fields"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
)

const (
	// defaultVerifyChunkSize is how many blocks the background verifier checks per iteration.
	// Each chunk re-fetches the range's logs once (plus headers/receipts only for blocks that
	// returned none), so a larger chunk means fewer round-trips at the cost of a bigger burst.
	defaultVerifyChunkSize = 5000

	// defaultVerifyChunkDelay paces the verifier between chunks to keep it off the hot path and
	// gentle on the EL, trading verification latency for lower steady-state load.
	defaultVerifyChunkDelay = 200 * time.Millisecond

	// Backoff bounds for retrying a transient verification failure in-process (see
	// verifyWithRetry), so a brief EL hiccup doesn't leave a range unverified until the next
	// restart. Capped exponential: quick first retry, backing off to gentle steady polling.
	defaultVerifyRetryInitialDelay = 10 * time.Second
	defaultVerifyRetryMaxDelay     = 5 * time.Minute

	// defaultVerifyUnsettledRecheckMaxDelay caps the backoff (starting at
	// defaultVerifyRetryInitialDelay) for re-checking unsettled blocks, and is how often an idle
	// verifier checks back for new ones. While no source can settle them a re-check costs about one
	// request, so the cap mainly trades how often their warning repeats for how soon a fix is
	// noticed.
	defaultVerifyUnsettledRecheckMaxDelay = time.Hour

	// defaultResyncCooldown rate-limits automatic repair: after a resync is flagged, further
	// confirmed misses within this window are suppressed (logged, not acted on). This bounds a
	// fatal+wipe+resync loop on a persistently-broken EL and caps a correlated network-wide
	// resync from a common-mode EL fault. During the cooldown the node keeps running on
	// possibly-incomplete state, with a loud log telling the operator to investigate.
	defaultResyncCooldown = 6 * time.Hour
)

// ErrResyncRequired signals that background verification found registry events the sync had
// missed. Registry events are order-dependent and carry per-owner nonces, so they cannot be
// repaired in place; the caller must restart into a clean, inline-verified resync. The
// resync-required flag is persisted before this is returned, so the next start performs it.
var ErrResyncRequired = errors.New("registry resync required: background verification found missing events")

// errResyncSuppressed is an internal signal that a receipts-confirmed miss was not acted on
// because a resync was flagged too recently (rate-limited); the block stays unsettled instead.
var errResyncSuppressed = errors.New("resync suppressed by rate limit")

// Verify runs one background verification pass. It first re-checks the blocks no source could
// settle before (see settleUnsettled), then verifies every optimistically-synced range: it
// re-fetches the contract's logs (VerifyLogs) and compares them, block by block, to the log IDs
// the optimistic sync recorded. Blocks that agree are retired. A disagreeing block is settled
// against receipts — the index-independent source of truth — and a resync is flagged
// (ErrResyncRequired) only when receipts confirm the sync genuinely missed events. A block with no
// authoritative verdict (no usable receipts from any client, or a bloom match nothing could
// settle) is recorded as unsettled rather than resynced or retired on unauthoritative evidence,
// and the range moves on. Progress is persisted per chunk, so an interrupted pass resumes where it
// left off. RunVerifier is the long-running form; both run off the startup critical path.
//
// The residual blind spot: if the sync and the verify-time re-fetch drop the same logs, their
// log IDs agree, no disagreement is raised, and receipts are never consulted — an identical
// double-drop goes undetected. Resolving every block against receipts would close it but is far
// more expensive, and the partial within-block drop it guards against has not been observed.
func (es *EventSyncer) Verify(ctx context.Context) error {
	_, _, err := es.verifyPass(ctx)
	return err
}

// verifyPass is Verify, additionally reporting how many blocks remain unsettled and whether the
// pass made progress (settled a block or verified a range).
func (es *EventSyncer) verifyPass(ctx context.Context) (unsettled int, progressed bool, err error) {
	settled, err := es.settleUnsettled(ctx)
	progressed = settled > 0
	if err != nil {
		return 0, progressed, err
	}

	ranges, err := es.nodeStorage.ListUnverifiedRanges(nil)
	if err != nil {
		return 0, progressed, fmt.Errorf("list unverified ranges: %w", err)
	}
	recordVerifyPendingRanges(ctx, len(ranges))
	if len(ranges) > 0 {
		es.logger.Info("verifying completeness of optimistically-synced registry events",
			zap.Int("pending_ranges", len(ranges)))
	}
	for _, r := range ranges {
		if err := es.verifyRange(ctx, r); err != nil {
			return 0, progressed, err // ErrResyncRequired or a genuine failure; either way, stop verifying.
		}
		progressed = true
	}
	recordVerifyPendingRanges(ctx, 0)

	pending, err := es.nodeStorage.ListUnsettledBlocks(nil)
	if err != nil {
		return 0, progressed, fmt.Errorf("list unsettled blocks: %w", err)
	}
	recordVerifyUnsettledBlocks(ctx, len(pending))

	switch {
	case len(pending) > 0:
		es.logger.Warn("background verification couldn't authoritatively verify some blocks yet; they stay pending and will be re-checked",
			zap.Int("unsettled_blocks", len(pending)),
			zap.String("recommendation", "usually no execution client can serve these blocks' receipts — use one that serves eth_getBlockReceipts"),
		)
	case progressed:
		es.logger.Info("background verification complete: optimistically-synced registry events are consistent with chain data")
	}
	return len(pending), progressed, nil
}

// RunVerifier runs background verification for as long as ctx lives. Once nothing is pending it
// checks back every verifyUnsettledRecheckMaxDelay, since streaming can record unsettled blocks at
// any time. It returns ErrResyncRequired when a miss is confirmed, or ctx's error once canceled.
func (es *EventSyncer) RunVerifier(ctx context.Context) error {
	for {
		if err := es.verifyWithRetry(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(es.verifyUnsettledRecheckMaxDelay):
		}
	}
}

// verifyWithRetry runs verification passes until no range or unsettled block remains (nil), a
// miss is confirmed (ErrResyncRequired), or ctx is canceled. Transient failures are retried with
// capped exponential backoff; unsettled blocks are re-checked on a slower-capped one until they
// settle, since what left them unsettled (an EL lacking or pruning receipts, a client being down,
// a rate-limited repair) can clear without a restart. The failure backoff resets after an
// error-free pass, the unsettled one after a pass that made progress.
func (es *EventSyncer) verifyWithRetry(ctx context.Context) error {
	failureBackoff := es.verifyRetryInitialDelay
	unsettledBackoff := es.verifyRetryInitialDelay
	for {
		unsettled, progressed, err := es.verifyPass(ctx)
		if progressed {
			unsettledBackoff = es.verifyRetryInitialDelay
		}

		var wait time.Duration
		switch {
		case errors.Is(err, ErrResyncRequired), errors.Is(err, context.Canceled):
			return err
		case err != nil:
			wait = failureBackoff
			failureBackoff = min(failureBackoff*2, es.verifyRetryMaxDelay)
			es.logger.Warn("background verification failed; retrying",
				zap.Duration("retry_in", wait), zap.Error(err))
		case unsettled > 0:
			failureBackoff = es.verifyRetryInitialDelay
			wait = unsettledBackoff
			unsettledBackoff = min(unsettledBackoff*2, es.verifyUnsettledRecheckMaxDelay)
			es.logger.Debug("re-checking unsettled blocks after a backoff",
				zap.Int("unsettled_blocks", unsettled), zap.Duration("retry_in", wait))
		default:
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// settleUnsettled re-checks unsettled blocks against receipts, oldest first, retiring those that
// settle clean. It stops at the first block no client can serve receipts for, since the source is
// likely still down, so polling a large backlog costs about one request. A confirmed miss returns
// ErrResyncRequired.
func (es *EventSyncer) settleUnsettled(ctx context.Context) (settled int, err error) {
	blocks, err := es.nodeStorage.ListUnsettledBlocks(nil)
	if err != nil {
		return 0, fmt.Errorf("list unsettled blocks: %w", err)
	}
	for _, block := range blocks {
		recorded, err := es.recordedLogIDs(block)
		if err != nil {
			return settled, err
		}
		clean, answered, err := es.settle(ctx, block, recorded)
		if err != nil {
			return settled, err
		}
		if !answered {
			break
		}
		if !clean {
			continue
		}
		if err := es.retireUnsettled(block); err != nil {
			return settled, err
		}
		settled++
	}
	return settled, nil
}

// verifyRange verifies a single journalled range chunk by chunk, resuming from its persisted
// cursor, and retires it once fully checked. Blocks without an authoritative verdict are recorded
// as unsettled rather than holding the range back. It returns ErrResyncRequired when receipts
// confirm a miss, or a wrapped error on RPC/storage failure.
func (es *EventSyncer) verifyRange(ctx context.Context, r nodestorage.UnverifiedRange) error {
	cursor := max(r.Cursor, r.From)
	for cursor <= r.To {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		toBlock := min(cursor+es.verifyChunkSize-1, r.To)

		fetched, err := es.executionClient.VerifyLogs(ctx, cursor, toBlock)
		if err != nil {
			return fmt.Errorf("verify logs [%d,%d]: %w", cursor, toBlock, err)
		}
		verifyTimeIDs := make(map[uint64][]executionclient.LogID, len(fetched))
		unsettledByFetch := make(map[uint64]bool)
		for _, bl := range fetched {
			if bl.Unsettled {
				unsettledByFetch[bl.BlockNumber] = true
			} else {
				verifyTimeIDs[bl.BlockNumber] = executionclient.BlockLogIDs(bl.Logs)
			}
		}

		// Compare every block in the chunk against its recorded log IDs, collecting the blocks whose
		// IDs can be dropped once the chunk commits and those left unsettled.
		var retired, unsettled []uint64
		for block := cursor; block <= toBlock; block++ {
			recorded, err := es.recordedLogIDs(block)
			if err != nil {
				return err
			}

			var verified bool
			switch {
			case unsettledByFetch[block]:
				// VerifyLogs already asked every client for this block's receipts, to no avail.
			case slices.Equal(recorded, verifyTimeIDs[block]):
				// Fast path: the verify-time fetch agrees with what the optimistic sync recorded.
				verified = true
			default:
				// They disagree. A verify-time getLogs isn't authoritative for a block (VerifyLogs
				// only recovers blocks that returned nothing, and under MultiClient the two passes may
				// hit different ELs), so settle the block against receipts before doing anything drastic.
				verified, _, err = es.settle(ctx, block, recorded)
				if err != nil {
					return err // ErrResyncRequired or a genuine failure.
				}
			}

			switch {
			case !verified:
				unsettled = append(unsettled, block)
			case len(recorded) > 0:
				retired = append(retired, block)
			}
		}

		nextCursor := toBlock + 1
		if err := es.commitChunk(r, nextCursor, retired, unsettled); err != nil {
			return err
		}
		recordVerifyCursor(ctx, toBlock)
		cursor = nextCursor

		if cursor <= r.To {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(es.verifyChunkDelay):
			}
		}
	}
	return nil
}

// settle checks a block against receipts, the index-independent source of truth, given the log
// IDs the sync recorded for it (none if it saw no logs there). clean reports that they match;
// answered is false when no client could serve authoritative receipts. Recorded IDs the receipts
// strictly contain are a confirmed miss: settle flags a resync and returns ErrResyncRequired,
// unless the rate limit holds the repair back. Recorded IDs missing from the receipts mean the
// receipts response is inconsistent (getLogs never invents logs), so the block stays unsettled
// rather than triggering a wipe.
func (es *EventSyncer) settle(ctx context.Context, block uint64, recorded []executionclient.LogID) (clean, answered bool, err error) {
	receiptLogs, available, err := es.executionClient.BlockContractLogs(ctx, block)
	if err != nil {
		return false, false, fmt.Errorf("settle block %d against receipts: %w", block, err)
	}
	if !available {
		es.logger.Debug("no authoritative receipts to settle a block; it stays unsettled",
			fields.BlockNumber(block))
		return false, false, nil
	}

	truth := executionclient.BlockLogIDs(receiptLogs)
	switch {
	case slices.Equal(recorded, truth):
		return true, true, nil
	case !isSubset(recorded, truth):
		es.logger.Warn("receipts lack logs the sync recorded for a block, so they can't settle it; it stays unsettled",
			fields.BlockNumber(block),
			zap.Int("recorded_logs", len(recorded)),
			zap.Int("receipt_logs", len(truth)),
		)
		return false, true, nil
	}

	err = es.flagResync(ctx, block, "receipts hold events the sync missed")
	if errors.Is(err, errResyncSuppressed) {
		return false, true, nil
	}
	return false, true, err
}

// isSubset reports whether every ID in a is also in b.
func isSubset(a, b []executionclient.LogID) bool {
	in := make(map[executionclient.LogID]bool, len(b))
	for _, id := range b {
		in[id] = true
	}
	for _, id := range a {
		if !in[id] {
			return false
		}
	}
	return true
}

// recordedLogIDs returns the log IDs the optimistic sync recorded for a block, or nil if none.
func (es *EventSyncer) recordedLogIDs(block uint64) ([]executionclient.LogID, error) {
	raw, found, err := es.nodeStorage.GetBlockLogIDs(nil, block)
	if err != nil {
		return nil, fmt.Errorf("get log IDs for block %d: %w", block, err)
	}
	if !found {
		return nil, nil
	}
	ids, err := executionclient.DecodeLogIDs(raw)
	if err != nil {
		return nil, fmt.Errorf("decode log IDs for block %d: %w", block, err)
	}
	return ids, nil
}

// commitChunk persists a verified chunk in one transaction — drops the retired blocks' recorded
// log IDs, records the unsettled blocks, and advances the range's cursor (removing the range once
// past its end) — so an interrupted run resumes without re-checking or leaking the chunk.
func (es *EventSyncer) commitChunk(r nodestorage.UnverifiedRange, nextCursor uint64, retired, unsettled []uint64) error {
	txn := es.nodeStorage.Begin()
	defer txn.Discard()

	for _, block := range retired {
		if err := es.nodeStorage.DeleteBlockLogIDs(txn, block); err != nil {
			return fmt.Errorf("delete log IDs for block %d: %w", block, err)
		}
	}
	for _, block := range unsettled {
		if err := es.nodeStorage.SaveUnsettledBlock(txn, block); err != nil {
			return fmt.Errorf("record unsettled block %d: %w", block, err)
		}
	}
	if nextCursor > r.To {
		if err := es.nodeStorage.DeleteUnverifiedRange(txn, r.From); err != nil {
			return fmt.Errorf("delete verified range: %w", err)
		}
	} else {
		r.Cursor = nextCursor
		if err := es.nodeStorage.SaveUnverifiedRange(txn, r); err != nil {
			return fmt.Errorf("save verification cursor: %w", err)
		}
	}
	return txn.Commit()
}

// retireUnsettled drops a settled block's unsettled mark and recorded log IDs in one transaction.
func (es *EventSyncer) retireUnsettled(block uint64) error {
	txn := es.nodeStorage.Begin()
	defer txn.Discard()

	if err := es.nodeStorage.DeleteUnsettledBlock(txn, block); err != nil {
		return fmt.Errorf("delete unsettled block %d: %w", block, err)
	}
	if err := es.nodeStorage.DeleteBlockLogIDs(txn, block); err != nil {
		return fmt.Errorf("delete log IDs for block %d: %w", block, err)
	}
	return txn.Commit()
}

// flagResync records a receipts-confirmed miss. Normally it persists the resync-required flag
// together with its time (so a crash can't leave the flag without the timestamp the rate limit
// reads) and returns ErrResyncRequired so the node restarts into a repair. But if a resync was already flagged within
// the cooldown it suppresses this one — returning errResyncSuppressed so the block stays unsettled
// — to bound a fatal+wipe+resync loop on a persistently-broken EL and to cap a correlated
// network-wide resync from a common-mode fault. During the cooldown the node keeps running on
// possibly-incomplete state; the block's first re-check after the cooldown flags the repair.
func (es *EventSyncer) flagResync(ctx context.Context, block uint64, reason string) error {
	last, found, err := es.nodeStorage.GetLastResyncTime(nil)
	if err != nil {
		return fmt.Errorf("get last resync time: %w", err)
	}
	if found && time.Since(last) < es.resyncCooldown {
		recordVerifySuppressed(ctx)
		es.logger.Warn("background verification found missing registry events, but a resync was flagged recently — suppressing the repair (rate-limited); the node keeps running on possibly-incomplete state, investigate the execution client",
			fields.BlockNumber(block),
			zap.String("reason", reason),
			zap.Duration("cooldown", es.resyncCooldown),
			zap.Time("last_resync", last),
		)
		return errResyncSuppressed
	}

	txn := es.nodeStorage.Begin()
	defer txn.Discard()
	if err := es.nodeStorage.SetResyncRequired(txn); err != nil {
		return fmt.Errorf("set resync-required flag: %w", err)
	}
	if err := es.nodeStorage.SetLastResyncTime(txn, time.Now()); err != nil {
		return fmt.Errorf("set last resync time: %w", err)
	}
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit resync flag: %w", err)
	}

	recordVerifyMiss(ctx)
	es.logger.Warn("background verification found the sync missed registry events; a full resync will run on the next start",
		fields.BlockNumber(block),
		zap.String("reason", reason),
	)
	return ErrResyncRequired
}
