package incoming

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

func (s *ReqHandler) handleGetDecidedReq(msg *network.SyncChanObj) {
	if len(msg.Msg.Params) != 2 {
		panic("implement")
	}
	if msg.Msg.Params[0] > msg.Msg.Params[1] {
		panic("implement")
	}

	// enforce max page size
	startSeq := msg.Msg.Params[0]
	endSeq := msg.Msg.Params[1]
	if endSeq-startSeq > s.paginationMaxSize {
		endSeq = startSeq + s.paginationMaxSize
	}

	ret := make([]*proto.SignedMessage, 0)
	for i := startSeq; i <= endSeq; i++ {
		decidedMsg, err := s.storage.GetDecided(s.identifier, i)
		if err != nil {
			s.logger.Error("failed to get decided", zap.Error(err), zap.ByteString("identifier", s.identifier), zap.Uint64("sequence", i))
			continue
		}

		ret = append(ret, decidedMsg)
	}

	retMsg := &network.SyncMessage{
		SignedMessages: ret,
		Lambda:         s.identifier,
		Type:           network.Sync_GetInstanceRange,
	}
	if err := s.network.RespondToGetDecidedByRange(msg.Stream, retMsg); err != nil {
		s.logger.Error("failed to send get decided by range response", zap.Error(err))
	}
}
