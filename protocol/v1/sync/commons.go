package sync

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
)

// GetHighest returns the highest message from the given collection
func GetHighest(logger *zap.Logger, remoteMsgs ...p2pprotocol.SyncResult) (highest *specqbft.SignedMessage, sender string) {
	var height specqbft.Height

	for _, remoteMsg := range remoteMsgs {
		sm, err := ExtractSyncMsg(remoteMsg.Msg)
		if err != nil {
			logger.Warn("bad sync message", zap.Error(err))
			continue
		}
		if sm == nil {
			continue
		}
		if len(sm.Data) == 0 {
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
func ExtractSyncMsg(msg *spectypes.SSVMessage) (*message.SyncMessage, error) {
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
	return sm, nil
}

// GetHighestSignedMessage returns the highest decided among the given set of messages.
// assuming all messages are of the same identifier
func GetHighestSignedMessage(signedMsgs ...*specqbft.SignedMessage) *specqbft.SignedMessage {
	var highest *specqbft.SignedMessage
	for _, msg := range signedMsgs {
		if msg == nil || msg.Message == nil {
			continue
		}
		if highest == nil {
			highest = msg
			continue
		}
		// if higher or (equal + more signers) then update highest
		if msg.Message.Height > highest.Message.Height {
			highest = msg
		} else if msg.Message.Height == highest.Message.Height && len(msg.GetSigners()) > len(highest.GetSigners()) {
			highest = msg
		} else {
			// older
		}
	}
	return highest
}
