package local

import (
	"fmt"
	"github.com/bloxapp/ssv/network"
	"sync/atomic"
	"time"
)

var testStreamCounter int64

// Stream is used by local network
type Stream struct {
	id          string
	From        string
	To          string
	ReceiveChan chan *network.SyncMessage
}

// NewLocalStream returs a stream instance
func NewLocalStream(From string, To string) network.SyncStream {
	return &Stream{
		id:          fmt.Sprintf("id-%d", atomic.AddInt64(&testStreamCounter, 1)),
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

// ReadWithTimeout implementation
func (s *Stream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	panic("implement")
}

// WriteWithTimeout implementation
func (s *Stream) WriteWithTimeout(data []byte, timeout time.Duration) error {
	panic("implement")
}

// ID implementation
func (s *Stream) ID() string {
	return s.id
}
