package history

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
)

// GetLastDecided reads last decided message from store
type GetLastDecided func(i message.Identifier) (*message.SignedMessage, error)

// DecidedHandler handles incoming decided messages
type DecidedHandler func(*message.SignedMessage) error

// History takes care for syncing decided history
type History interface {
	// SyncDecided syncs decided message with other peers in the network
	SyncDecided(ctx context.Context, identifier message.Identifier, getLastDecided GetLastDecided, handler DecidedHandler) (*message.SignedMessage, error)
	// SyncDecidedRange syncs decided messages for the given identifier and range
	SyncDecidedRange(ctx context.Context, identifier message.Identifier, handler DecidedHandler, from, to message.Height, targetPeers ...string) error
}

// history implements History
type history struct {
	logger *zap.Logger
	syncer p2pprotocol.Syncer
}

// New creates a new instance of History
func New(logger *zap.Logger, syncer p2pprotocol.Syncer) History {
	return &history{
		logger: logger,
		syncer: syncer,
	}
}

func (h *history) SyncDecided(ctx context.Context, identifier message.Identifier, getLastDecided GetLastDecided, handler DecidedHandler) (*message.SignedMessage, error) {
	logger := h.logger.With(zap.String("identifier", fmt.Sprintf("%x", identifier)))
	var err error
	var remoteMsgs []p2pprotocol.SyncResult
	retries := 2
	for retries > 0 && len(remoteMsgs) == 0 {
		retries--
		remoteMsgs, err = h.syncer.LastDecided(identifier)
		if err != nil {
			return nil, errors.Wrap(err, "could not fetch local highest instance during sync")
		}
		if len(remoteMsgs) == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if len(remoteMsgs) == 0 {
		logger.Info("node is synced: remote highest decided not found (V0)")
		return nil, nil
	}

	localMsg, err := getLastDecided(identifier)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}

	highest, height, sender := h.getHighest(localMsg, remoteMsgs...)
	if highest == nil {
		logger.Info("node is synced: remote highest decided not found (V1)")
		return nil, nil
	}

	if height <= localHeight {
		logger.Info("node is synced: local is higher or equal to remote")
		return highest, nil
	}

	logger.Debug("syncing decided range...", zap.Int64("local height", int64(localHeight)), zap.Int64("remote height", int64(height)))
	err = h.SyncDecidedRange(ctx, identifier, handler, localHeight, height, sender)
	if err != nil {
		// in optimistic approach we ignore failures and updates last decided message
		h.logger.Debug("could not get decided in range, skipping", zap.Error(err),
			zap.Int64("from", int64(localHeight)), zap.Int64("to", int64(height)))
	} else {
		logger.Debug("node is synced: remote highest found", zap.Int64("height", int64(height)))
	}
	return highest, nil
}

func (h *history) SyncDecidedRange(ctx context.Context, identifier message.Identifier, handler DecidedHandler, from, to message.Height, targetPeers ...string) error {
	visited := make(map[message.Height]bool)
	msgs, err := h.syncer.GetHistory(identifier, from, to, targetPeers...)
	if err != nil {
		return err
	}

	for _, msg := range msgs {
		if ctx.Err() != nil {
			break
		}
		sm, err := extractSyncMsg(msg.Msg)
		if err != nil {
			h.logger.Warn("failed to extract sync msg", zap.Error(err))
			continue
		}

	signedMsgLoop:
		for _, signedMsg := range sm.Data {
			height := signedMsg.Message.Height
			if err := handler(signedMsg); err != nil {
				h.logger.Warn("could not save decided", zap.Error(err), zap.Int64("height", int64(height)))
				continue
			}
			if visited[height] {
				continue signedMsgLoop
			}
			visited[height] = true
		}
	}
	if len(visited) != int(to-from)+1 {
		h.logger.Warn("not all messages in range", zap.Any("visited", visited), zap.Uint64("to", uint64(to)), zap.Uint64("from", uint64(from)))
		return errors.Errorf("not all messages in range were saved (%d out of %d)", len(visited), int(to-from))
	}
	return nil
}

func (h *history) getHighest(localMsg *message.SignedMessage, remoteMsgs ...p2pprotocol.SyncResult) (highest *message.SignedMessage, height message.Height, sender string) {
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}
	height = localHeight

	for _, remoteMsg := range remoteMsgs {
		sm, err := extractSyncMsg(remoteMsg.Msg)
		if err != nil {
			h.logger.Warn("bad sync message", zap.Error(err))
			continue
		}
		if sm == nil {
			h.logger.Debug("sync message not found because node is synced")
			continue
		}
		if len(sm.Data) == 0 {
			h.logger.Warn("empty sync message")
			continue
		}
		signedMsg := sm.Data[0]
		if signedMsg != nil && signedMsg.Message != nil && signedMsg.Message.Height > height {
			highest = signedMsg
			height = highest.Message.Height
			sender = remoteMsg.Sender
		}
	}
	return
}

func extractSyncMsg(msg *message.SSVMessage) (*message.SyncMessage, error) {
	sm := &message.SyncMessage{}
	err := sm.Decode(msg.Data)
	if err != nil {
		return nil, errors.Wrap(err, "could not decode sync message")
	}
	if sm.Status == message.StatusNotFound {
		return nil, nil
	}
	if sm.Status != message.StatusSuccess {
		return nil, errors.Errorf("failed to get sync message: %s", sm.Status.String())
	}
	if len(sm.Data) == 0 {
		return nil, errors.New("empty decided message")
	}
	return sm, nil
}
