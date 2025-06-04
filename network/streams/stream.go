package streams

import (
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core"
	"github.com/pkg/errors"
)

// Stream represents a stream in the system
type Stream interface {
	core.Stream

	// ReadWithTimeout will read bytes from stream and return the result, will return error if timeout or error.
	// does not close stream when returns
	ReadWithTimeout(timeout time.Duration) ([]byte, error)

	// WriteWithTimeout will write bytes to stream, will return error if timeout or error.
	// does not close stream when returns
	WriteWithTimeout(data []byte, timeout time.Duration) error
}

// streamWrapper is a wrapper struct for the core.Stream interface, implements Stream and network.SSVStream
type streamWrapper struct {
	core.Stream
}

// NewStream returns a new instance of stream
func NewStream(s core.Stream) Stream {
	return &streamWrapper{
		Stream: s,
	}
}

// ReadWithTimeout reads with timeout
func (ts *streamWrapper) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	if err := ts.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, errors.Wrap(err, "could not set read deadline")
	}
	return io.ReadAll(ts.Stream)
}

// WriteWithTimeout reads next message with timeout
func (ts *streamWrapper) WriteWithTimeout(data []byte, timeout time.Duration) error {
	if err := ts.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return errors.Wrap(err, "could not set write deadline")
	}

	n := len(data)
	bytesWritten, err := ts.Write(data)
	if bytesWritten != n {
		return errors.Errorf("written bytes (%d) to sync stream doesnt match input data (%d)", bytesWritten, n)
	}

	return err
}
