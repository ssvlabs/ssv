package syncing

import (
	"context"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/utils/tasks"
)

//go:generate mockgen -package=mocks -destination=./mocks/syncer.go -source=./syncer.go

// MessageHandler reacts to a message received from Syncer.
type MessageHandler func(msg spectypes.SSVMessage)

// Syncer handles the syncing of decided messages.
type Syncer interface {
	SyncHighestDecided(ctx context.Context, logger *zap.Logger, id spectypes.MessageID, handler MessageHandler) error
	SyncDecidedByRange(
		ctx context.Context,
		logger *zap.Logger,
		id spectypes.MessageID,
		from, to specqbft.Height,
		handler MessageHandler,
	) error
}

// Network is a subset of protocolp2p.Syncer, required by Syncer to retrieve messages from peers.
type Network interface {
	LastDecided(logger *zap.Logger, id spectypes.MessageID) ([]protocolp2p.SyncResult, error)
	GetHistory(
		logger *zap.Logger,
		id spectypes.MessageID,
		from, to specqbft.Height,
		targets ...string,
	) ([]protocolp2p.SyncResult, specqbft.Height, error)
}

type syncer struct {
	network Network
}

// New returns a standard implementation of Syncer.
func New(network Network) Syncer {
	return &syncer{
		network: network,
	}
}

func (s *syncer) SyncHighestDecided(
	ctx context.Context,
	logger *zap.Logger,
	id spectypes.MessageID,
	handler MessageHandler,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	logger = logger.With(
		zap.String("what", "SyncHighestDecided"),
		fields.PubKey(id.GetPubKey()),
		fields.Role(id.GetRoleType()))

	lastDecided, err := s.network.LastDecided(logger, id)
	if err != nil {
		logger.Debug("last decided sync failed", zap.Error(err))
		return errors.Wrap(err, "could not sync last decided")
	}
	if len(lastDecided) == 0 {
		logger.Debug("no messages were synced")
		return nil
	}

	results := protocolp2p.SyncResults(lastDecided)
	var maxHeight specqbft.Height
	results.ForEachSignedMessage(func(m *specqbft.SignedMessage) (stop bool) {
		if ctx.Err() != nil {
			return true
		}
		if m.Message.Height > maxHeight {
			maxHeight = m.Message.Height
		}
		raw, err := m.Encode()
		if err != nil {
			logger.Debug("could not encode signed message", zap.Error(err))
			return false
		}
		handler(spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   id,
			Data:    raw,
		})
		return false
	})
	logger.Debug("synced last decided", zap.Uint64("highest_height", uint64(maxHeight)), zap.Int("messages", len(lastDecided)))
	return nil
}

func (s *syncer) SyncDecidedByRange(
	ctx context.Context,
	logger *zap.Logger,
	id spectypes.MessageID,
	from, to specqbft.Height,
	handler MessageHandler,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	logger = logger.With(
		zap.String("what", "SyncDecidedByRange"),
		fields.PubKey(id.GetPubKey()),
		fields.Role(id.GetRoleType()),
		zap.Uint64("from", uint64(from)),
		zap.Uint64("to", uint64(to)))
	logger.Debug("syncing decided by range")

	err := s.getDecidedByRange(
		context.Background(),
		logger,
		id,
		from,
		to,
		func(sm *specqbft.SignedMessage) error {
			raw, err := sm.Encode()
			if err != nil {
				logger.Debug("could not encode signed message", zap.Error(err))
				return nil
			}
			handler(spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   id,
				Data:    raw,
			})
			return nil
		},
	)
	if err != nil {
		logger.Debug("sync failed", zap.Error(err))
	}
	return err
}

// getDecidedByRange calls GetHistory in batches to retrieve all decided messages in the given range.
func (s *syncer) getDecidedByRange(
	ctx context.Context,
	logger *zap.Logger,
	mid spectypes.MessageID,
	from, to specqbft.Height,
	handler func(*specqbft.SignedMessage) error,
) error {
	const maxRetries = 2

	var (
		visited = make(map[specqbft.Height]struct{})
		msgs    []protocolp2p.SyncResult
	)

	tail := from
	var err error
	for tail < to {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := tasks.RetryWithContext(ctx, func() error {
			start := time.Now()
			msgs, tail, err = s.network.GetHistory(logger, mid, tail, to)
			if err != nil {
				return err
			}
			handled := 0
			protocolp2p.SyncResults(msgs).ForEachSignedMessage(func(m *specqbft.SignedMessage) (stop bool) {
				if ctx.Err() != nil {
					return true
				}
				if _, ok := visited[m.Message.Height]; ok {
					return false
				}
				if err := handler(m); err != nil {
					logger.Warn("could not handle signed message")
				}
				handled++
				visited[m.Message.Height] = struct{}{}
				return false
			})
			logger.Debug("received and processed history batch",
				zap.Int64("tail", int64(tail)),
				fields.Duration(start),
				zap.Int("results_count", len(msgs)),
				fields.SyncResults(msgs),
				zap.Int("handled", handled))
			return nil
		}, maxRetries)
		if err != nil {
			return err
		}
	}

	return nil
}
