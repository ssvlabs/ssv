package streams

import (
	"github.com/bloxapp/ssv/network"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/pkg/errors"
	"io/ioutil"
	"time"
)

// timeoutStream is a wrapper struct for the core.Stream interface to match the network.SyncStream interface
type timeoutStream struct {
	s core.Stream
}

// NewTimeoutStream returns a new instance of stream
func NewTimeoutStream(s core.Stream) network.SyncStream {
	return &timeoutStream{
		s: s,
	}
}

// Close closes the stream
func (ts *timeoutStream) Close() error {
	return ts.s.Close()
}

// CloseWrite closes write stream
func (ts *timeoutStream) CloseWrite() error {
	return ts.s.CloseWrite()
}

// RemotePeer returns connected peer
func (ts *timeoutStream) RemotePeer() string {
	return ts.s.Conn().RemotePeer().String()
}

// ReadWithTimeout reads with timeout
func (ts *timeoutStream) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	if err := ts.s.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, errors.Wrap(err, "could not set read deadline")
	}
	return ioutil.ReadAll(ts.s)
}

// WriteWithTimeout reads with timeout
func (ts *timeoutStream) WriteWithTimeout(data []byte, timeout time.Duration) error {
	if err := ts.s.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return errors.Wrap(err, "could not set write deadline")
	}

	n := len(data)
	bytsWrote, err := ts.s.Write(data)
	if bytsWrote != n {
		return errors.Errorf("written bytes (%d) to sync stream doesnt match input data (%d)", bytsWrote, n)
	}
	return err
}

// ID returns the id of the stream
func (ts *timeoutStream) ID() string {
	return ts.s.ID()
}
