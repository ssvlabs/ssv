package history

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/sync"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// DecidedHandler handles incoming decided messages
type DecidedHandler func(*message.SignedMessage) error

// Syncer takes care for syncing decided history
type Syncer interface {
	// SyncRange syncs decided messages for the given identifier and range
	SyncRange(ctx context.Context, identifier message.Identifier, handler DecidedHandler, from, to message.Height, targetPeers ...string) error
}

// syncer implements Syncer
type syncer struct {
	logger *zap.Logger
	syncer p2pprotocol.Syncer
}

// NewSyncer creates a new instance of history syncer
func NewSyncer(logger *zap.Logger, netSyncer p2pprotocol.Syncer) Syncer {
	return &syncer{
		logger: logger,
		syncer: netSyncer,
	}
}

func (s syncer) SyncRange(ctx context.Context, identifier message.Identifier, handler DecidedHandler, from, to message.Height, targetPeers ...string) error {
	s.logger.Debug("fetching range history sync", zap.Int64("from", int64(from)), zap.Int64("to", int64(to)))
	visited := make(map[message.Height]bool)
	var msgs []p2pprotocol.SyncResult

	lastBatch := from
	var err error
	for lastBatch < to {
		// measuring sync batch process
		start := time.Now()
		msgs, lastBatch, err = s.syncer.GetHistory(identifier, lastBatch, to, targetPeers...)
		if err != nil {
			return err
		}
		s.processMessages(ctx, msgs, handler, visited)
		elapsed := time.Since(start)
		s.logger.Debug("received and processed history batch", zap.Int64("currentHighest", int64(lastBatch)), zap.Int64("needToSync", int64(to)), zap.Float64("duration", elapsed.Seconds()))
	}

	if len(visited) != int(to-from)+1 {
		s.logger.Warn("not all messages in range", zap.Any("visited", visited), zap.Uint64("to", uint64(to)), zap.Uint64("from", uint64(from)))
		return errors.Errorf("not all messages in range were saved (%d out of %d)", len(visited), int(to-from))
	}
	s.logger.Debug("done with range history sync", zap.Int("totalProcessed", len(visited)))
	return nil
}

func (s syncer) processMessages(ctx context.Context, msgs []p2pprotocol.SyncResult, handler DecidedHandler, visited map[message.Height]bool) {
	for _, msg := range msgs {
		if ctx.Err() != nil {
			break
		}
		sm, err := sync.ExtractSyncMsg(msg.Msg)
		if err != nil {
			s.logger.Warn("failed to extract sync msg", zap.Error(err))
			continue
		}
		if sm == nil {
			continue
		}
	signedMsgLoop:
		for _, signedMsg := range sm.Data {
			height := signedMsg.Message.Height
			if err := handler(signedMsg); err != nil {
				s.logger.Warn("could not save decided", zap.Error(err), zap.Int64("height", int64(height)))
				continue
			}
			if visited[height] {
				continue signedMsgLoop
			}
			visited[height] = true
		}
	}
}
