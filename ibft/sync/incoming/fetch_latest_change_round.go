package incoming

//
//import (
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/network"
//	"github.com/bloxapp/ssv/storage/kv"
//	"github.com/pkg/errors"
//	"go.uber.org/zap"
//)
//
//func (s *ReqHandler) handleGetLatestChangeRoundReq(msg *network.SyncChanObj) {
//	retMsg := &network.SyncMessage{
//		Lambda: s.identifier,
//		Type:   network.Sync_GetLatestChangeRound,
//	}
//
//	if err := s.validateGetLatestChangeRoundReq(msg); err != nil {
//		retMsg.Error = err.Error()
//	} else {
//		retMsg.SignedMessages = []*proto.SignedMessage{s.lastChangeRoundMsg}
//	}
//
//	if err := s.network.RespondSyncMsg(msg.StreamID, retMsg); err != nil {
//		s.logger.Error("failed to send current instance req", zap.Error(err))
//	}
//}
//
//func (s *ReqHandler) validateGetLatestChangeRoundReq(msg *network.SyncChanObj) error {
//	if s.seqNumber == -1 || s.lastChangeRoundMsg == nil {
//		return errors.New(kv.EntryNotFoundError)
//	}
//	if msg.Msg == nil {
//		return errors.New("sync msg invalid: sync msg nil")
//	}
//	if len(msg.Msg.Params) != 1 {
//		return errors.New("sync msg invalid: should be 2 elements")
//	}
//	if int64(msg.Msg.Params[0]) != s.seqNumber {
//		return errors.New(kv.EntryNotFoundError)
//	}
//	return nil
//}
