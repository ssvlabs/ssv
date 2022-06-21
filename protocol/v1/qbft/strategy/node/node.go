package node

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/sync/lastdecided"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type regularNode struct {
	logger         *zap.Logger
	store          qbftstorage.DecidedMsgStore
	decidedFetcher lastdecided.Fetcher
}

// NewRegularNodeStrategy creates a new instance of regular node strategy
func NewRegularNodeStrategy(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer) strategy.Decided {
	return &regularNode{
		logger:         logger.With(zap.String("who", "RegularNodeStrategy")),
		store:          store,
		decidedFetcher: lastdecided.NewLastDecidedFetcher(logger.With(zap.String("who", "LastDecidedFetcher")), syncer),
	}
}

func (f *regularNode) Sync(ctx context.Context, identifier message.Identifier, from, to *message.SignedMessage, pip pipelines.SignedMessagePipeline) error {
	if to == nil {
		highest, _, _, err := f.decidedFetcher.GetLastDecided(ctx, identifier, func(i message.Identifier) (*message.SignedMessage, error) {
			return from, nil
		})
		if err != nil {
			return errors.Wrap(err, "could not get last decided from peers")
		}
		to = highest
	}
	if to != nil {
		_, err := f.UpdateDecided(to)
		return errors.Wrap(err, "could not save decided")
	}
	return nil
}

func (f *regularNode) UpdateDecided(msg *message.SignedMessage) (*message.SignedMessage, error) {
	return strategy.UpdateLastDecided(f.logger, f.store, msg)
}

// GetDecided in regular node will try to look for last decided and returns it if in the given range
func (f *regularNode) GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error) {
	if len(heightRange) < 2 {
		return nil, errors.New("missing height range")
	}
	ld, err := f.store.GetLastDecided(identifier)
	if err != nil {
		return nil, err
	}
	if ld == nil {
		return nil, nil
	}
	height, from, to := ld.Message.Height, heightRange[0], heightRange[1]
	if height < from || height > to {
		return nil, nil
	}
	return []*message.SignedMessage{ld}, nil
}

func (f *regularNode) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	return f.store.GetLastDecided(identifier)
}
