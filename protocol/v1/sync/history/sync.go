package history

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ValidateDecided validates the given decided message
type ValidateDecided func(signedMessage *message.SignedMessage) error

type History interface {
	// SyncDecided syncs decided message with other peers in the network
	SyncDecided(identifier message.Identifier, optimistic bool) (bool, error)
	// SyncDecidedRange syncs decided messages for the given identifier and range
	SyncDecidedRange(identifier message.Identifier, from, to message.Height) (bool, error)
}

type history struct {
	logger          *zap.Logger
	store           qbftstorage.DecidedMsgStore
	syncer          p2pprotocol.Syncer
	validateDecided ValidateDecided
}

func New(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer, validateDecided ValidateDecided) History {
	return &history{
		logger:          logger,
		store:           store,
		syncer:          syncer,
		validateDecided: validateDecided,
	}
}

func (h *history) SyncDecided(identifier message.Identifier, optimistic bool) (bool, error) {
	logger := h.logger.With(zap.String("identifier", fmt.Sprintf("%x", identifier)))

	remoteMsgs, err := h.syncer.LastDecided(identifier)
	if err != nil {
		return false, errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	if len(remoteMsgs) == 0 {
		logger.Info("node is synced: remote highest decided not found, assuming sequence number is 0")
		return false, nil
	}

	localMsg, err := h.store.GetLastDecided(identifier)
	if err != nil && err.Error() != kv.EntryNotFoundError {
		return false, errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	var highest *message.SignedMessage
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}
	height := localHeight

	for _, remoteMsg := range remoteMsgs {
		sm, err := extractSyncMsg(remoteMsg)
		if err != nil {
			logger.Warn("bad sync message", zap.Error(err))
			continue
		}
		if sm.Data[0].Message.Height > height {
			highest = sm.Data[0]
			height = highest.Message.Height
		}
	}

	if height <= localHeight {
		logger.Info("node is synced")
		return false, nil
	}

	synced, err := h.SyncDecidedRange(identifier, localHeight, height)
	if err != nil {
		if !optimistic {
			return false, errors.Wrapf(err, "could not fetch and save decided in range [%d, %d]", localHeight, height)
		}
		// in optimistic approach we ignore failures and updates last decided message
		h.logger.Debug("could not get decided in range, skipping",
			zap.Int64("from", int64(localHeight)), zap.Int64("to", int64(height)))
	}

	err = h.store.SaveLastDecided(highest)
	if err != nil {
		return synced, errors.Wrapf(err, "could not save highest decided (%d)", height)
	}

	logger.Info("node is synced",
		zap.Int64("from", int64(localHeight)), zap.Int64("to", int64(height)))

	return synced, nil
}

func (h *history) SyncDecidedRange(identifier message.Identifier, from, to message.Height) (bool, error) {
	visited := make(map[message.Height]bool)
	msgs, err := h.syncer.GetHistory(identifier, from, to)
	if err != nil {
		return false, err
	}
	for _, msg := range msgs {
		sm, err := extractSyncMsg(msg)
		if err != nil {
			continue
		}
	signedMsgLoop:
		for _, signedMsg := range sm.Data {
			if err := h.validateDecided(signedMsg); err != nil {
				h.logger.Warn("message not valid", zap.Error(err))
				// TODO: report validation?
				continue signedMsgLoop
			}
			height := signedMsg.Message.Height
			if visited[height] {
				continue signedMsgLoop
			}
			if err := h.store.SaveDecided(signedMsg); err != nil {
				h.logger.Warn("could not save decided", zap.Error(err), zap.Int64("height", int64(height)))
			}
			visited[height] = true
		}
	}
	if len(visited) != int(to-from) {
		return false, errors.Errorf("not all messages in range were saved (%d out of %d)", len(visited), int(to-from))
	}
	return true, nil
}

func extractSyncMsg(msg *message.SSVMessage) (*message.SyncMessage, error) {
	sm := &message.SyncMessage{}
	err := sm.Decode(msg.Data)
	if err != nil {
		return nil, errors.Wrap(err, "could not decode sync message")
	}
	if len(sm.Data) == 0 {
		return nil, errors.New("empty decided message")
	}
	return sm, nil
}
