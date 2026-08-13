package operator

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/eventsyncer"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/localevents"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// syncContractEvents blocks until historical events are synced, then returns the event syncer plus a
// start-func for ongoing event sync (nil in local-events mode). The start-func runs until a clean
// ctx cancellation (→ nil) or a sync failure (→ a startupError carrying last_processed_block).
func syncContractEvents(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config,
	executionClient executionclient.Provider,
	validatorCtrl *validator.Controller,
	networkConfig *networkconfig.Network,
	nodeStorage operatorstorage.Storage,
	operatorDataStore operatordatastore.OperatorDataStore,
	keyManager ekm.KeyManager,
	doppelgangerHandler eventhandler.DoppelgangerProvider,
) (*eventsyncer.EventSyncer, func(context.Context) error, error) {
	eventFilterer, err := executionClient.Filterer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to set up event filterer: %w", err)
	}

	eventParser, err := eventparser.New(eventFilterer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create event parser: %w", err)
	}

	eventHandler, err := eventhandler.New(
		nodeStorage,
		eventParser,
		validatorCtrl,
		networkConfig,
		operatorDataStore,
		keyManager,
		doppelgangerHandler,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup event data handler: %w", err)
	}

	eventSyncer := eventsyncer.New(
		nodeStorage,
		executionClient,
		eventHandler,
		eventsyncer.WithLogger(logger),
	)

	// load & parse local events yaml if exists, otherwise sync from contract
	if len(cfg.LocalEventsPath) != 0 {
		localEvents, err := localevents.Load(cfg.LocalEventsPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load local events: %w", err)
		}

		if err := eventHandler.HandleLocalEvents(ctx, localEvents); err != nil {
			return nil, nil, fmt.Errorf("error occurred while running event data handler: %w", err)
		}

		// No ongoing sync in local-events mode.
		return eventSyncer, nil, nil
	}

	// If a previous run's background verification flagged a resync (or an earlier repair was
	// interrupted), drop/keep registry state as appropriate and rebuild below with inline
	// verification (verify=true).
	resyncing, err := prepareRegistryResync(nodeStorage, logger)
	if err != nil {
		return nil, nil, err
	}

	// Determine the block to resume contract sync from.
	fromBlock, found, err := nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("syncing registry contract events failed, could not get last processed block: %w", err)
	}
	if !found {
		fromBlock = networkConfig.RegistrySyncOffset
	} else if fromBlock == nil {
		return nil, nil, fmt.Errorf("syncing registry contract events failed, last processed block is nil")
	} else {
		// Start syncing from the next block.
		fromBlock = new(big.Int).SetUint64(fromBlock.Uint64() + 1)
	}

	// Decide whether to verify the historical sync inline. A restart catch-up small enough to
	// bloom-check cheaply is verified inline, so the node starts with guaranteed-complete state;
	// a large cold sync from the registry offset stays optimistic and is checked afterwards by
	// the background verifier. The resync-repair path always verifies inline.
	verify := resyncing || shouldVerifyCatchUpInline(ctx, executionClient, fromBlock.Uint64(), logger)

	logger.Debug("syncing historical registry events",
		zap.Uint64("fromBlock", fromBlock.Uint64()), zap.Bool("verify_inline", verify))
	lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, fromBlock.Uint64(), verify)
	switch {
	case errors.Is(err, executionclient.ErrNothingToSync):
		// Nothing was synced, keep fromBlock as is.
		logger.Info("finished syncing historical events, nothing to sync",
			zap.Uint64("from_block", fromBlock.Uint64()),
		)
	case err == nil:
		// Successfully synced up to a fresh block, advance fromBlock to the block after lastProcessedBlock.
		logger.Info("finished syncing historical events to a fresh block",
			zap.Uint64("from_block", fromBlock.Uint64()),
			zap.Uint64("last_processed_block", lastProcessedBlock),
		)
		fromBlock = new(big.Int).SetUint64(lastProcessedBlock + 1)
	default:
		return nil, nil, fmt.Errorf("failed to sync historical registry events: %w", err)
	}

	// The verified resync finished (we only reach here on success); clear both resync flags so the
	// next start is a normal boot. Clearing them only now — after completion — is what lets an
	// interrupted repair resume from the marker instead of restarting from scratch.
	if resyncing {
		if err := nodeStorage.ClearResyncRequired(nil); err != nil {
			return nil, nil, fmt.Errorf("failed to clear resync-required flag: %w", err)
		}
		if err := nodeStorage.ClearResyncInProgress(nil); err != nil {
			return nil, nil, fmt.Errorf("failed to clear resync-in-progress flag: %w", err)
		}
	}

	// Print registry stats.
	shares := nodeStorage.Shares().List(nil)
	operators, err := nodeStorage.ListOperatorsAll(nil)
	if err != nil {
		logger.Error("failed to get operators", zap.Error(err))
	}

	operatorValidators := 0
	liquidatedValidators := 0
	operatorID := operatorDataStore.GetOperatorID()
	if operatorDataStore.OperatorIDReady() {
		for _, share := range shares {
			if share.BelongsToOperator(operatorID) {
				operatorValidators++
			}
			if share.Liquidated {
				liquidatedValidators++
			}
		}
	}
	logger.Info("historical registry sync stats",
		zap.Uint64("my_operator_id", operatorID),
		zap.Int("operators", len(operators)),
		zap.Int("validators", len(shares)),
		zap.Int("liquidated_validators", liquidatedValidators),
		zap.Int("my_validators", operatorValidators),
	)

	// Sync ongoing registry events; the node must terminate if this stops. It can't operate
	// without staying current with Ethereum events, and until reorg handling exists, restarting
	// from persisted state is safer than continuing on possibly-incorrect state. A clean ctx
	// cancellation returns nil; any other error is returned as a startupError carrying
	// last_processed_block.
	startOngoingSync := func(ctx context.Context) error {
		g, ctx := errgroup.WithContext(ctx)

		// Verify any optimistically-synced ranges (large cold syncs) in the background, off the
		// critical path, checking each block's recorded log digest against chain data. It runs
		// concurrently with ongoing sync (below vs. above the last-processed marker) and retries
		// transient failures in-process. If it finds the optimistic sync missed events it returns
		// ErrResyncRequired: we surface that so the node terminates and restarts into the clean,
		// inline-verified resync (the flag is already persisted).
		g.Go(func() error {
			err := eventSyncer.VerifyWithRetry(ctx)
			switch {
			case err == nil, errors.Is(err, context.Canceled):
				return nil
			case errors.Is(err, eventsyncer.ErrResyncRequired):
				return startupError{
					err:    err,
					fields: []zap.Field{zap.Uint64("last_processed_block", lastProcessedBlock)},
				}
			default:
				// VerifyWithRetry only returns nil, a context error, or ErrResyncRequired, so this
				// is unexpected; log it and let the node keep running rather than crash.
				logger.Error("background verification stopped unexpectedly", zap.Error(err))
				return nil
			}
		})

		g.Go(func() error {
			if err := eventSyncer.SyncOngoing(ctx, fromBlock.Uint64()); err != nil && !errors.Is(err, context.Canceled) {
				return startupError{
					err:    fmt.Errorf("failed syncing ongoing registry events: %w", err),
					fields: []zap.Field{zap.Uint64("last_processed_block", lastProcessedBlock)},
				}
			}
			return nil
		})

		return g.Wait()
	}

	return eventSyncer, startOngoingSync, nil
}

// prepareRegistryResync decides whether the node should rebuild registry state with inline
// verification, and gets storage ready for it. Registry events are order-dependent and carry
// per-owner nonces, so they can't be patched in place — a full rebuild is the remedy.
//
// It handles two cases:
//   - A resync was flagged (a background-verification miss) but not yet started: drop registry
//     state and the stale verification journal, then mark the resync in progress. The drop
//     happens before the mark, so a crash in between just re-drops and restarts cleanly.
//   - A resync is already in progress (a previous repair was interrupted): do NOT drop again —
//     everything synced so far was inline-verified, so resume from the last-processed marker.
//
// Either way it returns true (rebuild with verify=true). The flags are cleared by the caller
// only once the resync completes, which is what makes an interrupted repair resumable.
func prepareRegistryResync(nodeStorage operatorstorage.Storage, logger *zap.Logger) (bool, error) {
	inProgress, err := nodeStorage.IsResyncInProgress(nil)
	if err != nil {
		return false, fmt.Errorf("failed to check resync-in-progress flag: %w", err)
	}
	if inProgress {
		logger.Warn("resuming an interrupted registry resync from the last processed block (its progress was inline-verified)")
		return true, nil
	}

	required, err := nodeStorage.IsResyncRequired(nil)
	if err != nil {
		return false, fmt.Errorf("failed to check resync-required flag: %w", err)
	}
	if !required {
		return false, nil
	}

	logger.Warn("registry resync required: background verification previously found missing events; dropping registry state and resyncing from scratch with inline verification")
	if err := nodeStorage.DropRegistryData(); err != nil {
		return false, fmt.Errorf("failed to drop registry data for resync: %w", err)
	}
	if err := nodeStorage.DropVerificationJournal(); err != nil {
		return false, fmt.Errorf("failed to drop verification journal for resync: %w", err)
	}
	if err := nodeStorage.SetResyncInProgress(nil); err != nil {
		return false, fmt.Errorf("failed to mark resync in progress: %w", err)
	}
	return true, nil
}

// maxInlineVerifyCatchUp is the largest historical catch-up (in blocks) we bloom-check inline
// on startup rather than syncing optimistically. Sized at roughly a week of mainnet blocks: it
// covers all but the most prolonged restarts — which then start with guaranteed-complete state
// instead of an optimistic window — while a cold sync from the registry offset stays optimistic
// (background-verified) so a fresh node isn't blocked on it. Tunable.
const maxInlineVerifyCatchUp = 50_000

// shouldVerifyCatchUpInline reports whether the historical catch-up from fromBlock is small
// enough to bloom-check inline (so the node starts with guaranteed-complete registry state)
// rather than sync it optimistically and rely on the background verifier. On any error sizing
// the range it returns false: optimistic is the safe, always-available default.
func shouldVerifyCatchUpInline(ctx context.Context, ec executionclient.Provider, fromBlock uint64, logger *zap.Logger) bool {
	head, err := ec.HeaderByNumber(ctx, nil)
	if err != nil || head == nil || head.Number == nil {
		logger.Debug("could not size historical catch-up; syncing optimistically", zap.Error(err))
		return false
	}

	headBlock := head.Number.Uint64()
	if headBlock < executionclient.FollowDistance {
		return true // below the follow distance there is nothing to sync; inline is trivially cheap
	}
	toBlock := headBlock - executionclient.FollowDistance
	if toBlock < fromBlock {
		return true // nothing to sync
	}
	return toBlock-fromBlock+1 <= maxInlineVerifyCatchUp
}
