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

// SyncDecided syncs decided message with other peers in the network
func SyncDecided(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer, validateDecided ValidateDecided, identifier message.Identifier) error {
	logger = logger.With(zap.String("identifier", fmt.Sprintf("%x", identifier)))
	localMsg, err := store.GetLastDecided(identifier)
	if err != nil && err.Error() != kv.EntryNotFoundError {
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	var highest *message.SignedMessage
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}
	height := localHeight

	remoteMsgs, err := syncer.LastDecided(identifier)
	if err != nil {
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}
	if len(remoteMsgs) == 0 {
		logger.Info("node is synced: remote highest decided not found, assuming sequence number is 0")
		return nil
	}

	for _, remoteMsg := range remoteMsgs {
		sm, err := getSyncMsg(remoteMsg)
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
		return nil
	}

	err = SyncDecidedRange(logger, store, syncer, validateDecided, identifier, localHeight, height)
	if err != nil {
		return errors.Wrapf(err, "could not fetch and save decided in range [%d, %d]", localHeight, height)
	}

	err = store.SaveLastDecided(highest)
	if err != nil {
		return errors.Wrapf(err, "could not save highest decided (%d)", height)
	}

	logger.Info("node is synced",
		zap.Int64("from", int64(localHeight)), zap.Int64("to", int64(height)))

	return nil
}

// SyncDecidedRange syncs decided messages for the given identifier and range
func SyncDecidedRange(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer, validateDecided ValidateDecided,
	identifier message.Identifier, from, to message.Height) error {
	visited := make(map[message.Height]bool)
	msgs, err := syncer.GetHistory(identifier, from, to)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		sm, err := getSyncMsg(msg)
		if err != nil {
			continue
		}
	signedMsgLoop:
		for _, signedMsg := range sm.Data {
			if err := validateDecided(signedMsg); err != nil {
				logger.Warn("message not valid", zap.Error(err))
				// TODO: report validation?
				continue signedMsgLoop
			}
			h := signedMsg.Message.Height
			if visited[h] {
				continue signedMsgLoop
			}
			if err := store.SaveDecided(signedMsg); err != nil {
				logger.Warn("could not save decided", zap.Error(err), zap.Int64("height", int64(h)))
			}
			visited[h] = true
		}
	}
	if len(visited) != int(to-from) {
		return errors.Errorf("not all messages in range were saved (%d out of %d)", len(visited), int(to-from))
	}
	return nil
}

func getSyncMsg(msg *message.SSVMessage) (*message.SyncMessage, error) {
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
