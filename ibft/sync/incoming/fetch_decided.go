package incoming

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (s *ReqHandler) handleGetDecidedReq(msg *network.SyncChanObj) {
	retMsg := &network.SyncMessage{
		Lambda: s.identifier,
		Type:   network.Sync_GetInstanceRange,
	}

	if err := s.validateGetDecidedReq(msg); err != nil {
		retMsg.Error = errors.Wrap(err, "invalid get decided request").Error()
	} else {
		// enforce max page size
		startSeq := msg.Msg.Params[0]
		endSeq := msg.Msg.Params[1]
		if endSeq-startSeq > s.paginationMaxSize {
			endSeq = startSeq + s.paginationMaxSize
		}

		ret := make([]*proto.SignedMessage, 0)
		for i := startSeq; i <= endSeq; i++ {
			decidedMsg, found, err := s.storage.GetDecided(s.identifier, i)
			logger := s.logger.With(zap.ByteString("identifier", s.identifier), zap.Uint64("sequence", i))
			if !found{
				logger.Error("decided was not found")
				continue
			}
			if err != nil {
				logger.Error("failed to get decided", zap.Error(err))
				continue
			}

			ret = append(ret, decidedMsg)
		}
		retMsg.SignedMessages = ret
	}

	if err := s.network.RespondToGetDecidedByRange(msg.Stream, retMsg); err != nil {
		s.logger.Error("failed to send get decided by range response", zap.Error(err))
	}
}

func (s *ReqHandler) validateGetDecidedReq(msg *network.SyncChanObj) error {
	if msg.Msg == nil {
		return errors.New("sync msg invalid: sync msg nil")
	}
	if len(msg.Msg.Params) != 2 {
		return errors.New("sync msg invalid: params should contain 2 elements")
	}
	if msg.Msg.Params[0] > msg.Msg.Params[1] {
		return errors.New("sync msg invalid: param[0] should be <= param[1]")
	}
	return nil
}
