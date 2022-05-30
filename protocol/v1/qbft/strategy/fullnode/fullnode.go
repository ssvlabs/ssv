package fullnode

import (
	"bytes"
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/sync/history"
	"github.com/bloxapp/ssv/protocol/v1/sync/lastdecided"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type fullNode struct {
	logger         *zap.Logger
	store          qbftstorage.DecidedMsgStore
	decidedFetcher lastdecided.Fetcher
	historySyncer  history.Syncer
}

// NewFullNodeStrategy creates a new instance of fullNode strategy
func NewFullNodeStrategy(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer) strategy.Decided {
	return &fullNode{
		logger:         logger.With(zap.String("who", "FullNodeStrategy")),
		store:          store,
		decidedFetcher: lastdecided.NewLastDecidedFetcher(logger.With(zap.String("who", "LastDecidedFetcher")), syncer),
		historySyncer:  history.NewSyncer(logger.With(zap.String("who", "HistorySyncer")), syncer),
	}
}

func (f *fullNode) Sync(ctx context.Context, identifier message.Identifier, pip pipelines.SignedMessagePipeline) error {
	logger := f.logger.With(zap.String("identifier", identifier.String()))
	highest, sender, localHeight, err := f.decidedFetcher.GetLastDecided(identifier, func(i message.Identifier) (*message.SignedMessage, error) {
		return f.store.GetLastDecided(i)
	})
	if err != nil {
		return errors.Wrap(err, "could not get last decided from peers")
	}
	logger.Debug("highest decided", zap.Int64("local", int64(localHeight)), zap.Any("highest", highest))
	if highest == nil {
		logger.Debug("could not find highest decided from peers")
		return nil
	}
	if localHeight > highest.Message.Height {
		return nil // local is higher than remote, no need for sync or update
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	logger.Debug("found higher remote than local: syncing range")
	counter := 0
	err = f.historySyncer.SyncRange(ctx, identifier, func(msg *message.SignedMessage) error {
		if err := pip.Run(msg); err != nil {
			return errors.Wrap(err, "invalid msg")
		}
		shouldUpdate, knownMsg, err := f.IsMsgKnown(msg)
		if err != nil {
			return errors.Wrap(err, "could not check if message is known")
		}
		if knownMsg != nil && !shouldUpdate { // msg is known but no need to update
			return nil
		}
		if err := f.store.SaveDecided(msg); err != nil {
			return errors.Wrap(err, "could not save decided msg to storage")
		}
		counter++
		return nil
	}, localHeight, highest.Message.Height, sender)
	if err != nil {
		return errors.Wrap(err, "could not complete sync")
	}
	warnMsg := ""
	if message.Height(counter-1) < highest.Message.Height-localHeight {
		warnMsg = "could not sync all messages in range"
	} else if message.Height(counter-1) > highest.Message.Height-localHeight {
		warnMsg = "got too many messages during sync"
	}
	if len(warnMsg) > 0 {
		logger.Warn(warnMsg,
			zap.Int("actual", counter), zap.Int64("from", int64(localHeight)),
			zap.Int64("to", int64(highest.Message.Height)))
	}

	if err == nil && highest != nil {
		return f.store.SaveLastDecided(highest)
	}
	return nil
}

func (f *fullNode) ValidateHeight(msg *message.SignedMessage) (bool, error) {
	lastDecided, err := f.store.GetLastDecided(msg.Message.Identifier)
	if err != nil {
		return false, errors.Wrap(err, "failed to get last decided")
	}
	if lastDecided == nil {
		return true, nil
	}
	if msg.Message.Height < lastDecided.Message.Height {
		return false, nil
	}
	return true, nil
}

// IsMsgKnown checks for known decided msg. Also checks if it needs to be updated
func (f *fullNode) IsMsgKnown(msg *message.SignedMessage) (bool, *message.SignedMessage, error) {
	msgs, err := f.store.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	if err != nil || len(msgs) == 0 || msgs[0] == nil {
		return false, nil, err
	}

	// if decided is known, check for a more complete message (more signers)
	knownMsg := msgs[0]
	if ignore := checkDecidedMessageSigners(knownMsg, msg); ignore {
		// if no more complete signers, check if data is different
		if shouldUpdate, err := checkDecidedData(knownMsg, msg); err != nil {
			return false, knownMsg, errors.Wrap(err, "failed to check decided data")
		} else if !shouldUpdate {
			return false, knownMsg, nil // both checks are false
		}
	}
	// one of the checks passed, need to update decided
	return true, knownMsg, nil
}

// checkDecidedMessageSigners checks if signers of existing decided includes all signers of the newer message
func checkDecidedMessageSigners(knownMsg *message.SignedMessage, msg *message.SignedMessage) bool {
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	return len(msg.GetSigners()) <= len(knownMsg.GetSigners())
}

// checkDecidedData checks if new decided msg target epoch is higher than the known decided target epoch
func checkDecidedData(knownMsg *message.SignedMessage, msg *message.SignedMessage) (bool, error) {
	knownCommitData, err := knownMsg.Message.GetCommitData()
	if err != nil {
		return false, errors.Wrap(err, "failed to get known msg commit data")
	}

	commitData, err := msg.Message.GetCommitData()
	if err != nil {
		return false, errors.Wrap(err, "failed to get new msg commit data")
	}
	return bytes.Compare(commitData.Data, knownCommitData.Data) != 0, nil
}

func (f *fullNode) SaveLateCommit(msg *message.SignedMessage) error {
	return f.store.SaveDecided(msg)
}

func (f *fullNode) UpdateDecided(msg *message.SignedMessage) error {
	return f.store.SaveDecided(msg)
}

func (f *fullNode) GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error) {
	if len(heightRange) < 2 {
		return nil, errors.New("missing height range")
	}
	return f.store.GetDecided(identifier, heightRange[0], heightRange[1])
}

func (f *fullNode) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	return f.store.GetLastDecided(identifier)
}

// SaveDecided in for fullnode saves both decided and last decided
func (f *fullNode) SaveDecided(signedMsgs ...*message.SignedMessage) error {
	if err := f.store.SaveDecided(signedMsgs...); err != nil {
		return errors.Wrap(err, "could not save decided msg to storage")
	}
	return strategy.SaveLastDecided(f.logger, f.store, signedMsgs...)
}
