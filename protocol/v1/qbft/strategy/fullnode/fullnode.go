package fullnode

import (
	"context"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/sync/history"
	"github.com/bloxapp/ssv/protocol/v1/sync/lastdecided"
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

func (f *fullNode) Sync(ctx context.Context, identifier message.Identifier, from, to *message.SignedMessage, pip pipelines.SignedMessagePipeline) error {
	logger := f.logger.With(zap.String("identifier", identifier.String()))
	highest, sender, localHeight, err := f.decidedFetcher.GetLastDecided(ctx, identifier, func(i message.Identifier) (*message.SignedMessage, error) {
		return from, nil
	})
	if err != nil {
		return errors.Wrap(err, "could not get last decided from peers")
	}
	//logger.Debug("highest decided", zap.Int64("local", int64(localHeight)),
	//	zap.Any("highest", highest), zap.Any("to", to))
	if highest == nil {
		logger.Debug("could not find highest decided from peers")
		if to == nil {
			return nil
		}
		highest = to
	}
	if localHeight >= highest.Message.Height {
		logger.Debug("local height is equal or higher than remote")
		return nil
	}

	counter := 0
	handleDecided := func(msg *message.SignedMessage) error {
		if err := pip.Run(msg); err != nil {
			return errors.Wrap(err, "invalid msg")
		}
		if updated, err := f.UpdateDecided(msg); err != nil {
			return errors.Wrap(err, "could not save decided msg to storage")
		} else if updated {
			counter++
		}
		return nil
	}
	// a special case where no need to sync, we already have highest so just need to handle it
	if localHeight+1 == highest.Message.Height {
		if err := handleDecided(highest); err != nil {
			return errors.Wrap(err, "could not handle highest decided")
		}
	} else if len(sender) == 0 {
		return errors.New("could not find peers to sync from")
	} else {
		err = f.historySyncer.SyncRange(ctx, identifier, handleDecided, localHeight, highest.Message.Height, sender)
		if err != nil {
			return errors.Wrap(err, "could not complete sync")
		}
	}

	if message.Height(counter) < highest.Message.Height-localHeight {
		logger.Warn("could not sync all messages in range",
			zap.Int("actual", counter), zap.Int64("from", int64(localHeight)),
			zap.Int64("to", int64(highest.Message.Height)))
	}

	_, err = strategy.SaveLastDecided(f.logger, f.store, highest)

	return err
}

func (f *fullNode) UpdateDecided(msg *message.SignedMessage) (bool, error) {
	localMsgs, err := f.GetDecided(msg.Message.Identifier, msg.Message.Height, msg.Message.Height)
	if err != nil {
		return false, errors.Wrap(err, "could not read decided")
	}
	if len(localMsgs) == 0 || localMsgs[0] == nil {
		return f.SaveDecided(msg)
	}
	localMsg := localMsgs[0]
	if msg.HasMoreSigners(localMsg) {
		return f.SaveDecided(msg)
	}
	return false, nil
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
func (f *fullNode) SaveDecided(signedMsgs ...*message.SignedMessage) (bool, error) {
	if err := f.store.SaveDecided(signedMsgs...); err != nil {
		return false, errors.Wrap(err, "could not save decided msg to storage")
	}
	return strategy.SaveLastDecided(f.logger, f.store, signedMsgs...)
}
