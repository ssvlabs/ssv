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

func (f *regularNode) Sync(ctx context.Context, identifier message.Identifier, knownMsg *message.SignedMessage, pip pipelines.SignedMessagePipeline) error {
	highest, _, _, err := f.decidedFetcher.GetLastDecided(ctx, identifier, func(i message.Identifier) (*message.SignedMessage, error) {
		return knownMsg, nil
	})
	if err != nil {
		return errors.Wrap(err, "could not get last decided from peers")
	}

	if highest != nil {
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
	local, err := f.store.GetLastDecided(msg.Message.Identifier)
	if err != nil {
		return false, nil, err
	}
	if local == nil {
		return false, nil, nil // local is nil anyway
	}
	if local.Message.Height == msg.Message.Height {
		if ignore := checkDecidedMessageSigners(local, msg); ignore {
			return false, local, nil
		}
		return true, local, nil
	}
	// if updated signers, return true
	return false, nil, nil // need to return nil msg in order to check force decided or sync
}

// checkDecidedMessageSigners checks if signers of existing decided includes all signers of the newer message
func checkDecidedMessageSigners(knownMsg *message.SignedMessage, msg *message.SignedMessage) bool {
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	return len(msg.GetSigners()) <= len(knownMsg.GetSigners())
}

func (f *regularNode) UpdateDecided(msg *message.SignedMessage) error {
	return f.SaveDecided(msg) // use the same func as SaveDecided func
}

func (f *regularNode) GetDecided(identifier message.Identifier, heightRange ...message.Height) ([]*message.SignedMessage, error) {
	ld, err := f.store.GetLastDecided(identifier)
	if err != nil {
		return nil, err
	}
	if ld == nil {
		return nil, nil
	}
	return []*message.SignedMessage{ld}, nil
}

func (f *regularNode) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	return f.store.GetLastDecided(identifier)
}

func (f *regularNode) SaveDecided(signedMsgs ...*message.SignedMessage) error {
	return strategy.SaveLastDecided(f.logger, f.store, signedMsgs...)
}
