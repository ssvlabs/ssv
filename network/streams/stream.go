package streams

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core"
)

// This package only carries the node-info handshake.
// Those payloads are only a few hundred bytes today, so 1 MiB is still a very generous limit.
const maxStreamMessageSize = 1 << 20 // 1 MiB

var ErrStreamMessageTooLarge = errors.New("stream message exceeds max size")

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
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	// Read one byte past the limit so oversized payloads are rejected instead of silently truncated.
	data, err := io.ReadAll(io.LimitReader(ts.Stream, maxStreamMessageSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStreamMessageSize {
		// Best-effort abort: the payload is invalid, and callers should handle ErrStreamMessageTooLarge
		// while suppressing the follow-up ErrReset on their deferred Close().
		_ = ts.Reset()
		return nil, ErrStreamMessageTooLarge
	}

	return data, nil
}

// WriteWithTimeout writes data to stream with timeout
func (ts *streamWrapper) WriteWithTimeout(data []byte, timeout time.Duration) error {
	if err := ts.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	n := len(data)
	bytesWritten, err := ts.Write(data)
	if err != nil {
		return fmt.Errorf("write stream: %w", err)
	}
	if bytesWritten != n {
		return fmt.Errorf("wrote %d of %d bytes: %w", bytesWritten, n, io.ErrShortWrite)
	}
	return nil
}
