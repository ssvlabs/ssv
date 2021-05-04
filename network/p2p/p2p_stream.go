package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"go.uber.org/zap"
	"io/ioutil"
)

func readMessageData(stream network.SyncStream) (*network.Message, error) {
	data := &network.Message{}
	buf, err := ioutil.ReadAll(stream)
	if err != nil {
		return nil, err
	}

	// unmarshal
	if err := json.Unmarshal(buf, data); err != nil {
		return nil, err
	}
	return data, nil
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
		cm, err := readMessageData(netSyncStream)
		if err != nil {
			n.logger.Error("could not read and parse stream", zap.Error(err))
			return
		}

		// send to listeners
		for _, ls := range n.listeners {
			go func(ls listener) {
				switch cm.Type {
				case network.NetworkMsg_SyncType:
					cm.SyncMessage.FromPeerID = stream.Conn().RemotePeer().String()
					ls.syncCh <- &network.SyncChanObj{Msg: cm.SyncMessage, Stream: netSyncStream}
				}
			}(ls)
		}
	})
}
