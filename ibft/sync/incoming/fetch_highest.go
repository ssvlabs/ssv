package incoming

//
//import (
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/network"
//	"github.com/bloxapp/ssv/storage/kv"
//	"go.uber.org/zap"
//)
//
//// handleGetHighestReq will return the highest known decided msg.
//// In case there isn't one, it will return a 0 length array
//func (s *ReqHandler) handleGetHighestReq(msg *network.SyncChanObj) {
//	res, err := s.getHighestDecided()
//	if err != nil {
//		s.logger.Error("failed to get highest decided from db", zap.String("fromPeer", msg.Msg.FromPeerID), zap.Error(err))
//	}
//
//	if err := s.network.RespondSyncMsg(msg.StreamID, res); err != nil {
//		s.logger.Error("failed to send highest decided response", zap.Error(err))
//	}
//}
//
//func (s *ReqHandler) getHighestDecided() (*network.SyncMessage, error) {
//	res := &network.SyncMessage{
//		Lambda: s.identifier,
//		Type:   network.Sync_GetHighestType,
//	}
//
//	highest, found, err := s.storage.GetHighestDecidedInstance(s.identifier)
//	if !found {
//		res.Error = kv.EntryNotFoundError // TODO: need to change once v0.0.12 is deprecated @see ibft/sync/history.go:163
//		err = nil                         // marking not-found as non error
//	} else {
//		signedMsg := make([]*proto.SignedMessage, 0)
//		if highest != nil {
//			signedMsg = append(signedMsg, highest)
//		}
//		res.SignedMessages = signedMsg
//	}
//
//	return res, err
//}
