package streams

import (
	core "github.com/libp2p/go-libp2p-core"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"time"
)

// Stream represents a stream in the system
type Stream interface {
	// ID returns the id of the stream
	ID() string

	io.Closer
	// CloseWrite closes the stream for writing but leaves it open for
	// reading. It does not free the stream, a call Close or
	// Reset is required.
	CloseWrite() error

	// ReadWithTimeout will read bytes from stream and return the result, will return error if timeout or error.
	// does not close stream when returns
	ReadWithTimeout(timeout time.Duration) ([]byte, error)

	// WriteWithTimeout will write bytes to stream, will return error if timeout or error.
	// does not close stream when returns
	WriteWithTimeout(data []byte, timeout time.Duration) error
}

// streamWrapper is a wrapper struct for the core.Stream interface, implements Stream and network.SSVStream
type streamWrapper struct {
	s core.Stream
}

// NewStream returns a new instance of stream
func NewStream(s core.Stream) Stream {
	return &streamWrapper{
		s: s,
	}
}

// Close closes the stream
func (ts *streamWrapper) Close() error {
	return ts.s.Close()
}

// CloseWrite closes write stream
func (ts *streamWrapper) CloseWrite() error {
	return ts.s.CloseWrite()
}

// ReadWithTimeout reads with timeout
func (ts *streamWrapper) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	if err := ts.s.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, errors.Wrap(err, "could not set read deadline")
	}
	return ioutil.ReadAll(ts.s)
}

// WriteWithTimeout reads next message with timeout
func (ts *streamWrapper) WriteWithTimeout(data []byte, timeout time.Duration) error {
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
func (ts *streamWrapper) ID() string {
	return ts.s.ID()
}
