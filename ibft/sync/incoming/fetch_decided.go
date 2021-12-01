package incoming

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
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
		t := time.Now()
		ret, err := s.storage.GetDecidedInRange(s.identifier, startSeq, endSeq)
		if err != nil {
			ret = make([]*proto.SignedMessage, 0)
		}
		s.logger.Debug("get decided in range", zap.Uint64("from", startSeq),
			zap.Uint64("to", endSeq), zap.Int64("time(ts)", time.Now().Sub(t).Milliseconds()),
			zap.String("identifier", string(s.identifier)), zap.Int("results count", len(ret)), zap.Error(err))
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
