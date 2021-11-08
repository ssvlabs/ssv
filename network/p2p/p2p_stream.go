package p2p

import (
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"go.uber.org/zap"
)

// syncStreamHandler sets a stream handler for the host to process streamed messages
func (n *p2pNetwork) syncStreamHandler() {
	n.host.SetStreamHandler(syncStreamProtocol, func(stream core.Stream) {
		n.logger.Debug("syncStreamHandler start")
		netSyncStream := NewSyncStream(stream)

		// read msg
		buf, err := netSyncStream.ReadWithTimeout(n.cfg.RequestTimeout)
		if err != nil {
			n.logger.Error("could not read incoming sync stream", zap.Error(err))
			return
		}

		n.logger.Debug("syncStreamHandler buf", zap.ByteString("buf", buf))

		cm, err := n.fork.DecodeNetworkMsg(buf)
		if err != nil {
			n.logger.Error("could not parse stream", zap.Error(err))
			return
		}
		n.logger.Debug("syncStreamHandler decoded", zap.Any("cm", cm))
		n.propagateSyncMsg(cm, netSyncStream)
	})
}

// propagateSyncMsg takes an incoming sync message and propagates it on the internal sync channel
func (n *p2pNetwork) propagateSyncMsg(cm *network.Message, netSyncStream network.SyncStream) {
	logger := n.logger.With(zap.String("func", "propagateSyncMsg"))
	// TODO: find a better way to deal with nil message
	// 	i.e. avoid sending nil messages in the network
	if netSyncStream == nil || cm == nil {
		logger.Debug("could not propagate nil message")
		return
	}
	cm.SyncMessage.FromPeerID = netSyncStream.RemotePeer()
	for _, ls := range n.listeners {
		go func(ls listener, nm network.Message) {
			switch nm.Type {
			case network.NetworkMsg_SyncType:
				if ls.syncCh != nil {
					ls.syncCh <- &network.SyncChanObj{
						Msg:    nm.SyncMessage,
						Stream: netSyncStream,
					}
				}
			}
		}(ls, *cm)
	}
}
