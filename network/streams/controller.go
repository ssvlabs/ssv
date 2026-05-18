package streams

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers/peertrace"
	"github.com/ssvlabs/ssv/observability/log/fields"
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
func NewStreamController(ctx context.Context, host host.Host, dialTimeout, readWriteTimeout time.Duration, peerObserver *peertrace.Observer) StreamController {
	ctrl := streamCtrl{
		ctx:              ctx,
		host:             host,
		dialTimeout:      dialTimeout,
		readWriteTimeout: readWriteTimeout,
		peerObserver:     peerObserver,
	}

	return &ctrl
}

type streamCtrl struct {
	ctx context.Context

	host host.Host

	dialTimeout      time.Duration
	readWriteTimeout time.Duration
	peerObserver     *peertrace.Observer
}

// Request sends a message to the given stream and returns the response
func (n *streamCtrl) Request(logger *zap.Logger, peerID peer.ID, protocol protocol.ID, data []byte) ([]byte, error) {
	// Dial with timeout.
	ctx, cancel := context.WithTimeout(n.ctx, n.dialTimeout)
	defer cancel()

	stream, err := n.host.NewStream(ctx, peerID, protocol)
	if err != nil {
		return nil, err
	}

	s := NewStream(stream)

	requestsSentCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(s.Protocol())))
	n.peerObserver.Observe(n.ctx, logger, "stream_request_sent", peerID,
		zap.String(fields.FieldProtocolID, string(s.Protocol())),
		zap.Int("payload_size", len(data)),
	)

	defer func() {
		if err := s.Close(); err != nil && !errors.Is(err, libp2pnetwork.ErrReset) {
			logger.Debug("could not close stream", zap.Error(err))
		}
	}()

	if err := s.WriteWithTimeout(data, n.readWriteTimeout); err != nil {
		return nil, fmt.Errorf("could not write to stream: %w", err)
	}
	if err := s.CloseWrite(); err != nil {
		return nil, fmt.Errorf("could not close write stream: %w", err)
	}
	res, err := s.ReadWithTimeout(n.readWriteTimeout)
	if err != nil {
		if errors.Is(err, ErrStreamMessageTooLarge) {
			n.observeOversizedPayload(logger, peerID, s.Protocol(), "response")
		}
		return nil, fmt.Errorf("could not read stream msg: %w", err)
	}

	responsesReceivedCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(s.Protocol())))
	n.peerObserver.Observe(n.ctx, logger, "stream_response_received", peerID,
		zap.String(fields.FieldProtocolID, string(s.Protocol())),
		zap.Int("payload_size", len(res)),
	)
	return res, nil
}

// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
// it returns functions to respond and close the stream
func (n *streamCtrl) HandleStream(logger *zap.Logger, stream core.Stream) ([]byte, StreamResponder, func(), error) {
	s := NewStream(stream)

	requestsReceivedCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(s.Protocol())))

	done := func() {
		if err := s.Close(); err != nil && !errors.Is(err, libp2pnetwork.ErrReset) {
			logger.Debug("failed to close stream (handler)", zap.String("s_id", s.ID()), zap.Error(err))
		}
	}

	data, err := s.ReadWithTimeout(n.readWriteTimeout)
	if err != nil {
		if errors.Is(err, ErrStreamMessageTooLarge) {
			n.observeOversizedPayload(logger, s.Conn().RemotePeer(), s.Protocol(), "request")
		}
		return nil, nil, done, fmt.Errorf("could not read stream msg: %w", err)
	}
	n.peerObserver.Observe(n.ctx, logger, "stream_request_received", s.Conn().RemotePeer(),
		zap.String(fields.FieldProtocolID, string(s.Protocol())),
		zap.Int("payload_size", len(data)),
	)

	return data, func(res []byte) error {
		cp := make([]byte, len(res))
		copy(cp, res)
		if err := s.WriteWithTimeout(cp, n.readWriteTimeout); err != nil {
			return fmt.Errorf("could not write to stream: %w", err)
		}

		responsesSentCounter.Add(n.ctx, 1, metric.WithAttributes(protocolIDAttribute(s.Protocol())))
		n.peerObserver.Observe(n.ctx, logger, "stream_response_sent", s.Conn().RemotePeer(),
			zap.String(fields.FieldProtocolID, string(s.Protocol())),
			zap.Int("payload_size", len(res)),
		)
		return nil
	}, done, nil
}

func (n *streamCtrl) observeOversizedPayload(logger *zap.Logger, peerID peer.ID, protocolID protocol.ID, direction string) {
	oversizedPayloadsCounter.Add(
		n.ctx,
		1,
		metric.WithAttributes(protocolIDAttribute(protocolID), streamDirectionAttribute(direction)),
	)
	logger.Warn(
		"rejected oversized stream payload",
		fields.PeerID(peerID),
		zap.String(fields.FieldProtocolID, string(protocolID)),
		zap.String("direction", direction),
	)
	n.peerObserver.Observe(n.ctx, logger, "stream_oversized_payload", peerID,
		zap.String(fields.FieldProtocolID, string(protocolID)),
		zap.String("direction", direction),
	)
}
