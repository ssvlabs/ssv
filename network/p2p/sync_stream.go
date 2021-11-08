package p2p

import (
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/pkg/errors"
	"io/ioutil"
	"time"
)

// syncStream is a wrapper struct for the core.Stream interface to match the network.SyncStream interface
type syncStream struct {
	stream core.Stream
}

// NewSyncStream returns a new instance of syncStream
func NewSyncStream(stream core.Stream) network.SyncStream {
	return &syncStream{stream: stream}
}

//// Read reads data to p
//func (s *syncStream) Read(p []byte) (n int, err error) {
//	return s.stream.Read(p)
//}
//
//// Write writes p to stream
//func (s *syncStream) Write(p []byte) (n int, err error) {
//	return s.stream.Write(p)
//}

// Close closes the stream
func (s *syncStream) Close() error {
	return s.stream.Close()
}

// CloseWrite closes write stream
func (s *syncStream) CloseWrite() error {
	return s.stream.CloseWrite()
}

// RemotePeer returns connected peer
func (s *syncStream) RemotePeer() string {
	return s.stream.Conn().RemotePeer().String()
}

// ReadWithTimeout reads with timeout
func (s *syncStream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	if err := s.stream.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, errors.Wrap(err, "could not set read deadline")
	}
	return ioutil.ReadAll(s.stream)
}

// WriteWithTimeout reads with timeout
func (s *syncStream) WriteWithTimeout(data []byte, timeout time.Duration) error {
	if err := s.stream.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return errors.Wrap(err, "could not set read deadline")
	}

	bytsWrote, err := s.stream.Write(data)
	if bytsWrote != len(data) {
		return errors.New("writen bytes to sync stream doesnt match input data")
	}
	return err
}
