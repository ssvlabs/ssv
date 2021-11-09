package p2p

import (
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (n *p2pNetwork) preStreamHandler(stream core.Stream) (*network.Message, network.SyncStream, error) {
	netSyncStream := NewSyncStream(stream)

	// read msg
	buf, err := netSyncStream.ReadWithTimeout(n.cfg.RequestTimeout)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not read incoming sync stream")
	}

	cm, err := n.fork.DecodeNetworkMsg(buf)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not parse stream")
	}
	return cm, netSyncStream, nil
}

func (n *p2pNetwork) setHighestDecidedStreamHandler() {
	n.host.SetStreamHandler(highestDecidedStream, func(stream core.Stream) {
		cm, s, err := n.preStreamHandler(stream)
		if err != nil {
			n.logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
			return
		}
		n.propagateSyncMsg(cm, s)
	})
}

func (n *p2pNetwork) setLegacyStreamHandler() {
	n.host.SetStreamHandler("/sync/0.0.1", func(stream core.Stream) {
		cm, s, err := n.preStreamHandler(stream)
		if err != nil {
			n.logger.Error(" highest decided preStreamHandler failed", zap.Error(err))
			return
		}
		n.propagateSyncMsg(cm, s)
	})
}

func (n *p2pNetwork) setDecidedByRangeStreamHandler() {
	n.host.SetStreamHandler(decidedByRangeStream, func(stream core.Stream) {
		cm, s, err := n.preStreamHandler(stream)
		if err != nil {
			n.logger.Error("decided by range preStreamHandler failed", zap.Error(err))
			return
		}
		n.propagateSyncMsg(cm, s)
	})
}

func (n *p2pNetwork) setLastChangeRoundStreamHandler() {
	n.host.SetStreamHandler(lastChangeRoundMsgStream, func(stream core.Stream) {
		cm, s, err := n.preStreamHandler(stream)
		if err != nil {
			n.logger.Error("last change round preStreamHandler failed", zap.Error(err))
			return
		}
		n.propagateSyncMsg(cm, s)
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
