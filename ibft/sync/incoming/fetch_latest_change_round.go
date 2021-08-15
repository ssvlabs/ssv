package incoming

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/kv"
	"go.uber.org/zap"
)

func (s *ReqHandler) handleGetLatestChangeRoundReq(msg *network.SyncChanObj) {
	retMsg := &network.SyncMessage{
		Lambda: s.identifier,
		Type:   network.Sync_GetLatestChangeRound,
	}

	if s.lastChangeRoundMsg != nil {
		retMsg.SignedMessages = []*proto.SignedMessage{s.lastChangeRoundMsg}
	} else {
		retMsg.Error = kv.EntryNotFoundError
	}

	if err := s.network.RespondToLastChangeRoundMsg(msg.Stream, retMsg); err != nil {
		s.logger.Error("failed to send current instance req", zap.Error(err))
	}
}
