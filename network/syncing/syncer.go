package syncing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:generate mockgen -package=mocks -destination=./mocks/syncer.go -source=./syncer.go

type MessageHandler func(msg spectypes.SSVMessage)

type Syncer interface {
	SyncHighestDecided(ctx context.Context, id spectypes.MessageID, handler MessageHandler) error
	SyncDecidedByRange(ctx context.Context, id spectypes.MessageID, from, to specqbft.Height, handler MessageHandler) error
}

type Network interface {
	LastDecided(id spectypes.MessageID) ([]protocolp2p.SyncResult, error)
	GetHistory(id spectypes.MessageID, from, to specqbft.Height, targets ...string) ([]protocolp2p.SyncResult, specqbft.Height, error)
}

type syncer struct {
	network Network
	logger  *zap.Logger
}

func New(logger *zap.Logger, network Network) Syncer {
	return &syncer{
		logger:  logger.With(zap.String("who", "Syncer")),
		network: network,
	}
}

func (s *syncer) SyncHighestDecided(ctx context.Context, id spectypes.MessageID, handler MessageHandler) error {
	logger := s.logger.With(
		zap.String("what", "SyncHighestDecided"),
		zap.String("identifier", id.String()))

	lastDecided, err := s.network.LastDecided(id)
	if err != nil {
		logger.Debug("sync failed", zap.Error(err))
		return errors.Wrap(err, "could not sync last decided")
	}
	if len(lastDecided) == 0 {
		logger.Debug("no messages were synced")
		return nil
	}

	results := protocolp2p.SyncResults(lastDecided)
	results.ForEachSignedMessage(func(m *specqbft.SignedMessage) {
		raw, err := m.Encode()
		if err != nil {
			logger.Debug("could not encode signed message", zap.Error(err))
			return
		}
		handler(spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   id,
			Data:    raw,
		})
	})
	return nil
}

func (s *syncer) SyncDecidedByRange(ctx context.Context, mid spectypes.MessageID, from, to qbft.Height, handler MessageHandler) error {
	logger := s.logger.With(
		zap.String("what", "SyncDecidedByRange"),
		zap.String("identifier", mid.String()),
		zap.Uint64("from", uint64(from)),
		zap.Uint64("to", uint64(to)))
	logger.Debug("syncing decided by range")

	err := s.getDecidedByRange(context.Background(), logger, mid, from, to, func(sm *specqbft.SignedMessage) error {
		raw, err := sm.Encode()
		if err != nil {
			logger.Debug("could not encode signed message", zap.Error(err))
			return nil
		}
		handler(spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   mid,
			Data:    raw,
		})
		return nil
	})
	if err != nil {
		logger.Debug("sync failed", zap.Error(err))
	}
	return err
}

func (s *syncer) getDecidedByRange(ctx context.Context, logger *zap.Logger, mid spectypes.MessageID, from, to specqbft.Height, handler func(*specqbft.SignedMessage) error) error {
	const getHistoryRetries = 2

	var (
		visited = make(map[specqbft.Height]struct{})
		msgs    []protocolp2p.SyncResult
	)

	tail := from
	var err error
	for tail < to {
		err := tasks.RetryWithContext(ctx, func() error {
			start := time.Now()
			msgs, tail, err = s.network.GetHistory(mid, tail, to)
			if err != nil {
				return err
			}
			handled := 0
			protocolp2p.SyncResults(msgs).ForEachSignedMessage(func(m *specqbft.SignedMessage) {
				if ctx.Err() != nil {
					return
				}
				if _, ok := visited[m.Message.Height]; ok {
					return
				}
				if err := handler(m); err != nil {
					logger.Warn("could not handle signed message")
				}
				handled++
				visited[m.Message.Height] = struct{}{}
			})
			logger.Debug("received and processed history batch",
				zap.Int64("tail", int64(tail)),
				zap.Duration("duration", time.Since(start)),
				zap.Int("results_count", len(msgs)),
				// TODO: remove this after testing
				zap.String("results", func() string {
					// Prints msgs as:
					//   "(type=1 height=1 round=1) (type=1 height=2 round=1) ..."
					var s []string
					for _, m := range msgs {
						var sm *specqbft.SignedMessage
						if m.Msg.MsgType == spectypes.SSVConsensusMsgType {
							sm = &specqbft.SignedMessage{}
							if err := sm.Decode(m.Msg.Data); err != nil {
								s = append(s, fmt.Sprintf("(%v)", err))
								continue
							}
							s = append(s, fmt.Sprintf("(type=%d height=%d round=%d)", m.Msg.MsgType, sm.Message.Height, sm.Message.Round))
						}
						s = append(s, fmt.Sprintf("(type=%d)", m.Msg.MsgType))
					}
					return strings.Join(s, ", ")
				}()),
				zap.Int("handled", handled))
			return nil
		}, getHistoryRetries)
		if err != nil {
			return err
		}
	}

	return nil
}
