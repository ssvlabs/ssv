package streams

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// StreamResponder abstracts the stream access with a simpler interface that accepts only the data to send
type StreamResponder func([]byte) error

// StreamController simplifies the interaction with libp2p streams.
type StreamController interface {
	// Request sends a message to the given stream and returns the response
	Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, msg []byte) ([]byte, error)
	// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
	HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, StreamResponder, func(), error)
}

// NewStreamController create a new instance of StreamController
func NewStreamController(ctx context.Context, host host.Host, dialTimeout, readWriteTimeout time.Duration) StreamController {
	ctrl := streamCtrl{
		ctx:              ctx,
		host:             host,
		dialTimeout:      dialTimeout,
		readWriteTimeout: readWriteTimeout,
	}

	return &ctrl
}

type streamCtrl struct {
	ctx context.Context

	host host.Host

	dialTimeout      time.Duration
	readWriteTimeout time.Duration
}

// Request sends a message to the given stream and returns the response
func (n *streamCtrl) Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, data []byte) ([]byte, error) {
	// Dial with timeout.
	ctx, cancel := context.WithTimeout(n.ctx, n.dialTimeout)
	defer cancel()

	s, err := n.host.NewStream(ctx, peerID, protocol)
	if err != nil {
		return nil, err
	}

	requestsSentCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(s.Protocol())))

	defer func() {
		if err := s.Close(); err != nil {
			logger.Debug("could not close stream", zap.Error(err))
		}
	}()
	stream := NewStream(s)

	if err := stream.WriteWithTimeout(data, n.readWriteTimeout); err != nil {
		return nil, errors.Wrap(err, "could not write to stream")
	}
	if err := s.CloseWrite(); err != nil {
		return nil, errors.Wrap(err, "could not close write stream")
	}
	res, err := stream.ReadWithTimeout(n.readWriteTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "could not read stream msg")
	}

	responsesReceivedCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(stream.Protocol())))
	return res, nil
}

// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
// it returns functions to respond and close the stream
func (n *streamCtrl) HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, StreamResponder, func(), error) {
	s := NewStream(stream)

	requestsReceivedCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(stream.Protocol())))

	done := func() {
		if err := s.Close(); err != nil && err.Error() != libp2pnetwork.ErrReset.Error() {
			logger.Debug("failed to close stream (handler)", zap.String("s_id", stream.ID()), zap.Error(err))
		}
	}

	data, err := s.ReadWithTimeout(n.readWriteTimeout)
	if err != nil {
		return nil, nil, done, errors.Wrap(err, "could not read stream msg")
	}

	return data, func(res []byte) error {
		cp := make([]byte, len(res))
		copy(cp, res)
		if err := s.WriteWithTimeout(cp, n.readWriteTimeout); err != nil {
			return errors.Wrap(err, "could not write to stream")
		}

		responsesSentCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(stream.Protocol())))
		return nil
	}, done, nil
}
