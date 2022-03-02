package topics

import (
	scrypto "github.com/bloxapp/ssv/utils/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"go.uber.org/zap"
)

// SSVMsgID returns msg_id for the given message
func SSVMsgID(msg []byte) string {
	// TODO: check performance
	h := scrypto.Sha256Hash(msg)
	return string(h[20:])
}

// MsgID calculates msg_id based on
func MsgID(logger *zap.Logger, mapper MsgMapper) func(pmsg *ps_pb.Message) string {
	return func(pmsg *ps_pb.Message) string {
		mid := SSVMsgID(pmsg.Data)
		if len(mid) == 0 {
			// use default msg_id in case we failed to create our msg_id
			return string(pmsg.From) + string(pmsg.Seqno)
		}
		pid, err := peer.IDFromBytes(pmsg.From)
		if err == nil {
			mapper.Add(mid, pid)
		} else {
			logger.Warn("could not convert to peer id", zap.ByteString("pmsg.From", pmsg.From),
				zap.Error(err))
		}
		return mid
	}
}
