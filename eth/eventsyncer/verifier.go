package eventsyncer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// VerifyWithRetry), so a brief EL hiccup doesn't leave a range unverified until the next
	// restart. Capped exponential: quick first retry, backing off to gentle steady polling.
	defaultVerifyRetryInitialDelay = 10 * time.Second
	defaultVerifyRetryMaxDelay     = 5 * time.Minute

	// defaultVerifyParkedRecheckMaxDelay caps the backoff (starting at defaultVerifyRetryInitialDelay)
	// for re-checking parked ranges. A re-check costs about one block's requests, so the cap mainly
	// trades how often an unresolved range repeats its warnings for how soon a fix is noticed.
	defaultVerifyParkedRecheckMaxDelay = time.Hour

	// defaultResyncCooldown rate-limits automatic repair: after a resync is flagged, further
	// confirmed misses within this window are suppressed (logged, not acted on). This bounds a
	// fatal+wipe+resync loop on a persistently-broken EL and caps a correlated network-wide
	// resync from a common-mode EL fault. During the cooldown the node keeps running on
	// possibly-incomplete state, with a loud log telling the operator to investigate.
	defaultResyncCooldown = 6 * time.Hour
)

// ErrResyncRequired signals that background verification found registry events the optimistic
// sync had missed. Registry events are order-dependent and carry per-owner nonces, so they
// cannot be repaired in place; the caller must restart into a clean, inline-verified resync.
// The resync-required flag is persisted before this is returned, so the next start performs it.
var ErrResyncRequired = errors.New("registry resync required: background verification found missing events")

// errResyncSuppressed is an internal signal that a receipts-confirmed miss was not acted on
// because a resync was flagged too recently (rate-limited); the range is parked instead.
var errResyncSuppressed = errors.New("resync suppressed by rate limit")

// Verify runs one background verification pass, checking that every optimistically-synced range
// is complete with respect to chain data. For each range it re-fetches the contract's logs
// (VerifyLogs) and compares them, block by block, to the digests the optimistic sync recorded.
// Blocks that agree are retired. On a disagreement it resolves the block against receipts — the
// index-independent source of truth — and flags a resync (ErrResyncRequired) only when receipts
// confirm the sync genuinely missed events; without an authoritative source it parks the range on
// that block (pending, visible on pending_ranges) rather than resyncing on unauthoritative evidence
// or claiming it verified. Progress is persisted per chunk, so an interrupted pass resumes where it
// left off. VerifyWithRetry is the long-running form; both run off the startup critical path.
//
// The residual blind spot: if the sync and the verify-time re-fetch drop the same logs, the
// digests agree, no disagreement is raised, and receipts are never consulted — an identical
// double-drop goes undetected. Resolving every block against receipts would close it but is far
// more expensive, and the partial within-block drop it guards against has not been observed.
func (es *EventSyncer) Verify(ctx context.Context) error {
	_, err := es.verifyPass(ctx)
	return err
}

// verifyPass is Verify, additionally reporting how many ranges it left parked.
func (es *EventSyncer) verifyPass(ctx context.Context) (parked int, err error) {
	ranges, err := es.nodeStorage.ListUnverifiedRanges(nil)
	if err != nil {
		return 0, fmt.Errorf("list unverified ranges: %w", err)
	}
	recordVerifyPendingRanges(ctx, len(ranges))
	if len(ranges) == 0 {
		return 0, nil
	}

	es.logger.Info("verifying completeness of optimistically-synced registry events",
		zap.Int("pending_ranges", len(ranges)))

	for _, r := range ranges {
		wasParked, err := es.verifyRange(ctx, r)
		if err != nil {
			return parked, err // ErrResyncRequired or a genuine failure; either way, stop verifying.
		}
		if wasParked {
			parked++
		}
	}

	// Whatever remains in the journal is the parked ranges (couldn't be authoritatively verified).
	remaining, err := es.nodeStorage.ListUnverifiedRanges(nil)
	if err != nil {
		return parked, fmt.Errorf("list unverified ranges: %w", err)
	}
	recordVerifyPendingRanges(ctx, len(remaining))

	if parked > 0 {
		es.logger.Warn("background verification could not authoritatively verify some ranges; they remain pending and will be re-checked (see earlier warnings)",
			zap.Int("parked_ranges", parked))
	} else {
		es.logger.Info("background verification complete: optimistically-synced registry events are consistent with chain data")
	}
	return parked, nil
}

// VerifyWithRetry runs verification passes until every journalled range is verified (nil), a miss
// is confirmed (ErrResyncRequired), or ctx is canceled. Transient failures are retried with capped
// exponential backoff; parked ranges are re-checked on a slower-capped one for as long as the node
// runs, since what parked them (an inconsistent receipts response, an EL lacking
// eth_getBlockReceipts, a rate-limited repair) can clear without a restart.
func (es *EventSyncer) VerifyWithRetry(ctx context.Context) error {
	failureBackoff := es.verifyRetryInitialDelay
	parkedBackoff := es.verifyRetryInitialDelay
	for {
		parked, err := es.verifyPass(ctx)

		var wait time.Duration
		switch {
		case errors.Is(err, ErrResyncRequired), errors.Is(err, context.Canceled):
			return err
		case err != nil:
			wait = failureBackoff
			failureBackoff = min(failureBackoff*2, es.verifyRetryMaxDelay)
			es.logger.Warn("background verification failed; retrying",
				zap.Duration("retry_in", wait), zap.Error(err))
		case parked > 0:
			wait = parkedBackoff
			parkedBackoff = min(parkedBackoff*2, es.verifyParkedRecheckMaxDelay)
			es.logger.Debug("re-checking parked ranges after a backoff",
				zap.Int("parked_ranges", parked), zap.Duration("retry_in", wait))
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

// verifyRange verifies a single journalled range chunk by chunk, resuming from its persisted
// cursor; a parked range first re-checks its parked block alone. It returns parked=true when it
// left the range pending because a disagreeing block couldn't be authoritatively resolved (no
// authoritative receipts, or a resync was rate-limited); ErrResyncRequired when receipts confirm a
// miss; nil once the whole range verifies clean (having removed it from the journal); or a wrapped
// error on RPC/storage failure.
func (es *EventSyncer) verifyRange(ctx context.Context, r nodestorage.UnverifiedRange) (parked bool, err error) {
	cursor := max(r.Cursor, r.From)
	recheckParked := r.Parked
	for cursor <= r.To {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		toBlock := min(cursor+es.verifyChunkSize-1, r.To)
		if recheckParked {
			// Likely still unresolved: re-check the parked block alone rather than a whole chunk.
			toBlock = cursor
			recheckParked = false
		}

		authoritative, err := es.executionClient.VerifyLogs(ctx, cursor, toBlock)
		if err != nil {
			return false, fmt.Errorf("verify logs [%d,%d]: %w", cursor, toBlock, err)
		}
		authDigests := make(map[uint64][]byte, len(authoritative))
		for _, bl := range authoritative {
			authDigests[bl.BlockNumber] = executionclient.BlockLogsDigest(bl.Logs)
		}

		// Compare every block in the chunk against its recorded digest, collecting blocks that
		// carry a digest so we can drop them once the chunk is confirmed clean.
		recorded := make([]uint64, 0, len(authoritative))
		for block := cursor; block <= toBlock; block++ {
			stored, found, err := es.nodeStorage.GetBlockLogDigest(nil, block)
			if err != nil {
				return false, fmt.Errorf("get block-log digest for block %d: %w", block, err)
			}
			authDigest, hasAuth := authDigests[block]

			// Fast path: the verify-time fetch agrees with what the optimistic sync recorded.
			if found == hasAuth && (!found || bytes.Equal(stored, authDigest)) {
				if found {
					recorded = append(recorded, block)
				}
				continue
			}

			// They disagree. A verify-time getLogs isn't authoritative for a block (VerifyLogs only
			// recovers blocks that returned nothing, and under MultiClient the two passes may hit
			// different ELs), so resolve the block against receipts before doing anything drastic.
			clean, err := es.resolveMismatch(ctx, block, stored, found)
			if errors.Is(err, errRangeParked) {
				// No authoritative resolution: commit progress up to here and park the range on
				// this block rather than resync on unauthoritative evidence.
				if err := es.commitVerifiedChunk(r, block, recorded, true); err != nil {
					return false, err
				}
				recordVerifyParked(ctx)
				return true, nil
			}
			if err != nil {
				return false, err // ErrResyncRequired or a genuine failure.
			}
			if clean {
				recorded = append(recorded, block)
			}
		}

		nextCursor := toBlock + 1
		if err := es.commitVerifiedChunk(r, nextCursor, recorded, false); err != nil {
			return false, err
		}
		recordVerifyCursor(ctx, toBlock)
		cursor = nextCursor

		if cursor <= r.To {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(es.verifyChunkDelay):
			}
		}
	}
	return false, nil
}

// errRangeParked signals verifyRange to leave the current range pending rather than resync,
// because a disagreeing block couldn't be authoritatively resolved.
var errRangeParked = errors.New("range parked: block could not be authoritatively verified")

// resolveMismatch settles a block whose recorded digest disagrees with the verify-time fetch, by
// consulting receipts (the index-independent source of truth). It returns clean=true when the
// recorded digest matches the receipts (the verify-time getLogs was merely incomplete);
// ErrResyncRequired when the recorded digest disagrees with the receipts (a confirmed miss);
// errRangeParked when there's no authoritative source (receipts unavailable) or a resync is
// rate-limited; or a wrapped error on RPC/storage failure.
func (es *EventSyncer) resolveMismatch(ctx context.Context, block uint64, stored []byte, found bool) (clean bool, err error) {
	receiptLogs, receiptsAvailable, err := es.executionClient.BlockContractLogs(ctx, block)
	if err != nil {
		return false, fmt.Errorf("resolve block %d against receipts: %w", block, err)
	}
	if !receiptsAvailable {
		es.logger.Warn("background verification: no authoritative receipts source to resolve a disagreeing block (eth_getBlockReceipts unsupported or unreliable); leaving range pending",
			fields.BlockNumber(block))
		return false, errRangeParked
	}

	// Compare what the sync recorded (a digest, or the empty digest if it recorded nothing) against
	// the receipts-derived truth.
	recordedDigest := executionclient.BlockLogsDigest(nil)
	if found {
		recordedDigest = stored
	}
	if bytes.Equal(recordedDigest, executionclient.BlockLogsDigest(receiptLogs)) {
		// Recorded matches the receipts-derived truth; only the verify-time getLogs was incomplete.
		return true, nil
	}

	// The recorded digest disagrees with the receipts truth — the optimistic sync genuinely missed
	// events (recorded is a subset of truth, so it can only be short, never over-recorded).
	err = es.flagResync(ctx, block, "receipts confirm the optimistic sync missed events")
	if errors.Is(err, errResyncSuppressed) {
		return false, errRangeParked
	}
	return false, err
}

// commitVerifiedChunk advances the range's cursor past a clean chunk and drops that chunk's
// now-checked digests in one transaction, so an interrupted run resumes without re-checking
// (nor leaking) it. Once the cursor passes the range end, the range itself is removed. parked
// records that the block at nextCursor couldn't be authoritatively resolved.
func (es *EventSyncer) commitVerifiedChunk(r nodestorage.UnverifiedRange, nextCursor uint64, recorded []uint64, parked bool) error {
	txn := es.nodeStorage.Begin()
	defer txn.Discard()

	for _, block := range recorded {
		if err := es.nodeStorage.DeleteBlockLogDigest(txn, block); err != nil {
			return fmt.Errorf("delete block-log digest for block %d: %w", block, err)
		}
	}
	if nextCursor > r.To {
		if err := es.nodeStorage.DeleteUnverifiedRange(txn, r.From); err != nil {
			return fmt.Errorf("delete verified range: %w", err)
		}
	} else {
		r.Cursor = nextCursor
		r.Parked = parked
		if err := es.nodeStorage.SaveUnverifiedRange(txn, r); err != nil {
			return fmt.Errorf("save verification cursor: %w", err)
		}
	}
	return txn.Commit()
}

// flagResync records a receipts-confirmed miss. Normally it persists the resync-required flag
// (and the flag time) and returns ErrResyncRequired so the node restarts into a repair. But if a
// resync was already flagged within the cooldown it suppresses this one — returning
// errResyncSuppressed so the caller parks the range — to bound a fatal+wipe+resync loop on a
// persistently-broken EL and to cap a correlated network-wide resync from a common-mode fault.
// During the cooldown the node keeps running on possibly-incomplete state; the first re-check of
// the parked range after the cooldown flags the repair.
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

	if err := es.nodeStorage.SetResyncRequired(nil); err != nil {
		return fmt.Errorf("set resync-required flag: %w", err)
	}
	if err := es.nodeStorage.SetLastResyncTime(nil, time.Now()); err != nil {
		return fmt.Errorf("set last resync time: %w", err)
	}
	recordVerifyMiss(ctx)
	es.logger.Warn("background verification found the optimistic sync missed registry events; a full resync will run on the next start",
		fields.BlockNumber(block),
		zap.String("reason", reason),
	)
	return ErrResyncRequired
}
