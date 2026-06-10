package operator

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"go.uber.org/zap"

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

	// Set only on the contract-sync path below; nil in local-events mode (no ongoing sync there).
	var startOngoingSync func(context.Context) error

	// load & parse local events yaml if exists, otherwise sync from contract
	if len(cfg.LocalEventsPath) != 0 {
		localEvents, err := localevents.Load(cfg.LocalEventsPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load local events: %w", err)
		}

		if err := eventHandler.HandleLocalEvents(ctx, localEvents); err != nil {
			return nil, nil, fmt.Errorf("error occurred while running event data handler: %w", err)
		}
	} else {
		// Sync historical registry events.
		logger.Debug("syncing historical registry events", zap.Uint64("fromBlock", fromBlock.Uint64()))
		lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, fromBlock.Uint64())
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
		startOngoingSync = func(ctx context.Context) error {
			if err := eventSyncer.SyncOngoing(ctx, fromBlock.Uint64()); err != nil && !errors.Is(err, context.Canceled) {
				return startupError{
					err:    fmt.Errorf("failed syncing ongoing registry events: %w", err),
					fields: []zap.Field{zap.Uint64("last_processed_block", lastProcessedBlock)},
				}
			}
			return nil
		}
	}

	return eventSyncer, startOngoingSync, nil
}
