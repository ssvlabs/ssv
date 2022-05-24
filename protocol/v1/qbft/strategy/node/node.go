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

func (f *regularNode) Sync(ctx context.Context, identifier message.Identifier, pip pipelines.SignedMessagePipeline) error {
	highest, _, _, err := f.decidedFetcher.GetLastDecided(ctx, identifier, func(i message.Identifier) (*message.SignedMessage, error) {
		return f.store.GetLastDecided(i)
	})
	if err != nil {
		return errors.Wrap(err, "could not get last decided from peers")
	}

	if err == nil && highest != nil {
		return f.store.SaveLastDecided(highest)
	}
	return nil
}

func (f *regularNode) ValidateHeight(msg *message.SignedMessage) (bool, error) {
	lastDecided, err := f.store.GetLastDecided(msg.Message.Identifier)
	if err != nil {
		return false, errors.Wrap(err, "failed to get last decided")
	}
	if lastDecided != nil && msg.Message.Height < lastDecided.Message.Height {
		return false, nil
	}
	return true, nil
}

func (f *regularNode) IsMsgKnown(msg *message.SignedMessage) (bool, *message.SignedMessage, error) {
	res, err := f.store.GetLastDecided(msg.Message.Identifier)
	if err != nil {
		return false, nil, err
	}
	if res == nil {
		return false, nil, nil
	}
	return true, res, nil
}

func (f *regularNode) SaveLateCommit(msg *message.SignedMessage) error {
	return f.store.SaveLastDecided(msg)
}

func (f *regularNode) UpdateDecided(msg *message.SignedMessage) error {
	return f.store.SaveLastDecided(msg)
}

func (f *regularNode) GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error) {
	ld, err := f.store.GetLastDecided(identifier)
	if err != nil {
		return nil, err
	}
	return []*message.SignedMessage{ld}, nil
}

func (f *regularNode) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	return f.store.GetLastDecided(identifier)
}

func (f *regularNode) SaveDecided(signedMsgs ...*message.SignedMessage) error {
	return strategy.SaveLastDecided(f.logger, f.store, signedMsgs...)
}
