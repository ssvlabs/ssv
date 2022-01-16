package p2p

import (
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// sendSyncRequest sends a sync request and returns the result
func (n *p2pNetwork) sendSyncRequest(peerStr string, protocol protocol.ID, msg *network.SyncMessage) (*network.Message, error) {
	peerID, err := peerFromString(peerStr)
	if err != nil {
		return nil, err
	}
	res, err := n.streamCtrl.Request(peerID, protocol, &network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to make sync request")
	}
	if res.SyncMessage == nil {
		return nil, errors.New("no response for sync request")
	}
	n.logger.Debug("got sync response",
		zap.String("FromPeerID", res.SyncMessage.GetFromPeerID()))
	return res, nil
}

// RespondSyncMsg responds to the given stream
func (n *p2pNetwork) RespondSyncMsg(streamID string, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty()
	return n.streamCtrl.Respond(&network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
		StreamID:    streamID,
	})
}

func (n *p2pNetwork) setLegacyStreamHandler() {
	n.host.SetStreamHandler("/sync/0.0.1", func(stream core.Stream) {
		cm, _, err := n.streamCtrl.HandleStream(stream)
		if err != nil {
			n.logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
			return
		}
		if cm == nil {
			n.logger.Debug("got nil sync message")
			return
		}
		// adjusting message and propagating to other (internal) components
		cm.SyncMessage.FromPeerID = stream.Conn().RemotePeer().String()
		go propagateSyncMessage(n.listeners.GetListeners(network.NetworkMsg_SyncType), cm)
	})
}

//func (n *p2pNetwork) setHighestDecidedStreamHandler() {
//	n.host.SetStreamHandler(highestDecidedStream, func(stream core.Stream) {
//		cm, s, err := n.preStreamHandler(stream)
//		if err != nil {
//			n.logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
//			return
//		}
//		n.propagateSyncMsg(cm, s)
//	})
//}
//
//func (n *p2pNetwork) setDecidedByRangeStreamHandler() {
//	n.host.SetStreamHandler(decidedByRangeStream, func(stream core.Stream) {
//		cm, s, err := n.preStreamHandler(stream)
//		if err != nil {
//			n.logger.Error("decided by range preStreamHandler failed", zap.Error(err))
//			return
//		}
//		n.propagateSyncMsg(cm, s)
//	})
//}
//
//func (n *p2pNetwork) setLastChangeRoundStreamHandler() {
//	n.host.SetStreamHandler(lastChangeRoundMsgStream, func(stream core.Stream) {
//		cm, s, err := n.preStreamHandler(stream)
//		if err != nil {
//			n.logger.Error("last change round preStreamHandler failed", zap.Error(err))
//			return
//		}
//		n.propagateSyncMsg(cm, s)
//	})
//}

// GetHighestDecidedInstance asks peers for SyncMessage
func (n *p2pNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	res, err := n.sendSyncRequest(peerStr, legacyMsgStream, msg)
	if err != nil || res == nil {
		return nil, err
	}
	return res.SyncMessage, nil
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (n *p2pNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	res, err := n.sendSyncRequest(peerStr, legacyMsgStream, msg)
	if err != nil {
		return nil, err
	}
	return res.SyncMessage, nil
}

// GetLastChangeRoundMsg returns the latest change round msg for a running instance, could return nil
func (n *p2pNetwork) GetLastChangeRoundMsg(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	res, err := n.sendSyncRequest(peerStr, legacyMsgStream, msg)
	if err != nil || res == nil {
		return nil, err
	}
	return res.SyncMessage, nil
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (n *p2pNetwork) ReceivedSyncMsgChan() (<-chan *network.SyncChanObj, func()) {
	ls := listeners.NewListener(network.NetworkMsg_SyncType)

	return ls.SyncChan(), n.listeners.Register(ls)
}
