package p2p

import (
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"go.uber.org/zap"
	"io/ioutil"
)

func (n *p2pNetwork) readMessageData(stream network.SyncStream) (*network.Message, error) {
	//data := &network.Message{}
	buf, err := ioutil.ReadAll(stream)
	if err != nil {
		return nil, err
	}

	return n.fork.DecodeNetworkMsg(buf)
}

// SyncStream is a wrapper struct for the core.Stream interface to match the network.SyncStream interface
type SyncStream struct {
	stream core.Stream
}

// Read reads data to p
func (s *SyncStream) Read(p []byte) (n int, err error) {
	return s.stream.Read(p)
}

// Write writes p to stream
func (s *SyncStream) Write(p []byte) (n int, err error) {
	return s.stream.Write(p)
}

// Close closes the stream
func (s *SyncStream) Close() error {
	return s.stream.Close()
}

// CloseWrite closes write stream
func (s *SyncStream) CloseWrite() error {
	return s.stream.CloseWrite()
}

// RemotePeer returns connected peer
func (s *SyncStream) RemotePeer() string {
	return s.stream.Conn().RemotePeer().String()
}

// handleStream sets a stream handler for the host to process streamed messages
func (n *p2pNetwork) handleStream() {
	n.host.SetStreamHandler(syncStreamProtocol, func(stream core.Stream) {
		netSyncStream := &SyncStream{stream: stream}
		cm, err := n.readMessageData(netSyncStream)
		if err != nil {
			n.logger.Error("could not read and parse stream", zap.Error(err))
			return
		}
		n.propagateSyncMsg(cm, netSyncStream)
	})
}

// propagateSyncMsg takes an incoming sync message and propagates it on the internal sync channel
func (n *p2pNetwork) propagateSyncMsg(cm *network.Message, netSyncStream *SyncStream) {
	logger := n.logger.With(zap.String("func", "propagateSyncMsg"))
	// TODO: find a better way to deal with nil message
	// 	i.e. avoid sending nil messages in the network
	if netSyncStream == nil || cm == nil {
		logger.Debug("could not propagate nil message")
		return
	}
	cm.SyncMessage.FromPeerID = netSyncStream.stream.Conn().RemotePeer().String()
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
