package local

import (
	"github.com/bloxapp/ssv/network"
	"time"
)

// Stream is used by local network
type Stream struct {
	From        string
	To          string
	ReceiveChan chan *network.SyncMessage
}

// NewLocalStream returs a stream instance
func NewLocalStream(From string, To string) network.SyncStream {
	return &Stream{
		From:        From,
		To:          To,
		ReceiveChan: make(chan *network.SyncMessage),
	}
}

// WriteSynMsg implementation
func (s *Stream) WriteSynMsg(msg *network.SyncMessage) (n int, err error) {
	s.ReceiveChan <- msg
	return 0, nil
}

// Close implementation
func (s *Stream) Close() error {
	panic("implement")
}

// CloseWrite implementation
func (s *Stream) CloseWrite() error {
	panic("implement")
}

// RemotePeer implementation
func (s *Stream) RemotePeer() string {
	panic("implement")
}

func (s *Stream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	panic("implement")
}

func (s *Stream) WriteWithTimeout(data []byte, timeout time.Duration) error {
	panic("implement")
}
