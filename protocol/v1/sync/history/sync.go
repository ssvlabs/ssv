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

func SyncHistory(logger *zap.Logger, store qbftstorage.DecidedMsgStore, syncer p2pprotocol.Syncer, identifier message.Identifier) error {
	localMsg, err := store.GetLastDecided(identifier)
	if err != nil && err.Error() != kv.EntryNotFoundError {
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}
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
		logger.Info("node is synced: could not find any peer with highest decided, assuming sequence number is 0")
		return nil
	}

	for _, remoteMsg := range remoteMsgs {
		sm := &message.SyncMessage{}
		err := sm.Decode(remoteMsg.Data)
		if err != nil {
			logger.Warn("could not decode sync message", zap.Error(err))
			continue
		}
		if len(sm.Data) == 0 {
			logger.Debug("empty decided message", zap.String("identifier", fmt.Sprintf("%x", remoteMsg.ID)))
			continue
		}
		if sm.Data[0].Message.Height > height {
			height = sm.Data[0].Message.Height
		}
	}
}
