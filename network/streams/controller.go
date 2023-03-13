package streams

import (
	"context"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"

	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/forks"
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
func NewStreamController(ctx context.Context, host host.Host, fork forks.Fork, requestTimeout time.Duration) StreamController {
	ctrl := streamCtrl{
		ctx:            ctx,
		host:           host,
		fork:           fork,
		requestTimeout: requestTimeout,
	}

	return &ctrl
}

type streamCtrl struct {
	ctx context.Context

	host host.Host
	fork forks.Fork

	requestTimeout time.Duration
}

// Request sends a message to the given stream and returns the response
func (n *streamCtrl) Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, data []byte) ([]byte, error) {
	s, err := n.host.NewStream(n.ctx, peerID, protocol)
	if err != nil {
		return nil, err
	}
	stream := NewStream(s)
	defer func() {
		err := stream.Close()
		if err != nil && err.Error() != libp2pnetwork.ErrReset.Error() {
			logger.Debug("failed to close stream (request)", zap.String("s_id", stream.ID()), zap.Error(err))
		}
	}()
	metricsStreamOutgoingRequests.WithLabelValues(string(protocol)).Inc()
	metricsStreamRequestsActive.WithLabelValues(string(protocol)).Inc()
	defer metricsStreamRequestsActive.WithLabelValues(string(protocol)).Dec()

	if err := stream.WriteWithTimeout(data, n.requestTimeout); err != nil {
		return nil, errors.Wrap(err, "could not write to stream")
	}
	if err := s.CloseWrite(); err != nil {
		return nil, errors.Wrap(err, "could not close write stream")
	}
	res, err := stream.ReadWithTimeout(n.requestTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "could not read stream msg")
	}
	metricsStreamRequestsSuccess.WithLabelValues(string(protocol)).Inc()
	return res, nil
}

// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
// it returns functions to respond and close the stream
func (n *streamCtrl) HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, StreamResponder, func(), error) {
	s := NewStream(stream)

	protocolID := stream.Protocol()
	metricsStreamRequests.WithLabelValues(string(protocolID)).Inc()
	// logger := logger.With(zap.String("protocol", string(protocolID)), zap.String("streamID", streamID))
	done := func() {
		if err := s.Close(); err != nil && err.Error() != libp2pnetwork.ErrReset.Error() {
			logger.Debug("failed to close stream (handler)", zap.String("s_id", stream.ID()), zap.Error(err))
		}
	}
	data, err := s.ReadWithTimeout(n.requestTimeout)
	if err != nil {
		return nil, nil, done, errors.Wrap(err, "could not read stream msg")
	}

	return data, func(res []byte) error {
		cp := make([]byte, len(res))
		copy(cp, res)
		if err := s.WriteWithTimeout(cp, n.requestTimeout); err != nil {
			// logger.Debug("could not write to stream", zap.Error(err))
			return errors.Wrap(err, "could not write to stream")
		}
		metricsStreamResponses.WithLabelValues(string(protocolID)).Inc()
		return nil
	}, done, nil
}
