package sync

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// GetHighest returns the highest message from the given collection
func GetHighest(logger *zap.Logger, localMsg *message.SignedMessage, remoteMsgs ...p2pprotocol.SyncResult) (highest *message.SignedMessage, height message.Height, sender string) {
	var localHeight message.Height
	if localMsg != nil {
		localHeight = localMsg.Message.Height
	}
	height = localHeight

	for _, remoteMsg := range remoteMsgs {
		sm, err := ExtractSyncMsg(remoteMsg.Msg)
		if err != nil {
			logger.Warn("bad sync message", zap.Error(err))
			continue
		}
		if sm == nil {
			logger.Debug("sync message not found")
			continue
		}
		if len(sm.Data) == 0 {
			logger.Warn("empty sync message")
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

// ExtractSyncMsg extracts message.SyncMessage from message.SSVMessage
func ExtractSyncMsg(msg *message.SSVMessage) (*message.SyncMessage, error) {
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
