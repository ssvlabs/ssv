package streams

import (
	"context"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/forks"
	core "github.com/libp2p/go-libp2p-core"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async"
	"go.uber.org/zap"
	"sync"
	"time"
)

var (
	pruneInterval = time.Minute * 5
	cacheTimeout  = time.Minute * 4
)

// StreamController simplifies the interaction with libp2p streams.
// it wraps and keeps a reference to the opened stream, then takes care of reading/writing when needed.
type StreamController interface {
	// RequestBytes sends a raw message to the given stream and returns the response
	// TODO: change after network.Message was updated
	RequestBytes(peerID peer.ID, protocol protocol.ID, msg []byte, timeout time.Duration) ([]byte, error)
	// Request sends a message to the given stream and returns the response
	Request(peerID peer.ID, protocol protocol.ID, msg *network.Message) (*network.Message, error)
	// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
	HandleStream(stream core.Stream) (*network.Message, network.SyncStream, error)
	// Respond responds to incoming message
	Respond(msg *network.Message) error
}

// NewStreamController create a new instance of StreamController
func NewStreamController(ctx context.Context, logger *zap.Logger, host host.Host,
	fork forks.Fork, requestTimeout time.Duration) StreamController {
	ctrl := streamCtrl{
		ctx:            ctx,
		logger:         logger,
		host:           host,
		fork:           fork,
		requestTimeout: requestTimeout,
		streams:        make(map[string]streamEntry),
		streamsLock:    &sync.Mutex{},
	}

	async.RunEvery(ctx, pruneInterval, ctrl.clean)

	return &ctrl
}

type streamCtrl struct {
	ctx context.Context

	logger *zap.Logger

	host host.Host
	fork forks.Fork

	streams     map[string]streamEntry
	streamsLock *sync.Mutex

	requestTimeout time.Duration
}

type streamEntry struct {
	s network.SyncStream
	t time.Time
}

// Request sends a message to the given stream and returns the response
func (n *streamCtrl) Request(peerID peer.ID, protocol protocol.ID, msg *network.Message) (*network.Message, error) {
	s, err := n.host.NewStream(n.ctx, peerID, protocol)
	if err != nil {
		return nil, err
	}
	stream := NewTimeoutStream(s)
	defer func() {
		if err := stream.Close(); err != nil {
			n.logger.Error("could not close stream", zap.Error(err))
		}
	}()
	metricsStreamRequests.Inc()
	metricsStreamRequestsActive.Inc()
	defer metricsStreamRequestsActive.Dec()

	if err := n.sendMsg(stream, msg); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, errors.Wrap(err, "could not close write stream")
	}

	res, err := n.readMsg(stream)
	if err != nil {
		return nil, err
	}
	metricsStreamRequestsSuccess.Inc()
	return res, nil
}

// RequestBytes sends a raw message to the given stream and returns the response
// TODO: change after network.Message was updated
func (n *streamCtrl) RequestBytes(peerID peer.ID, protocol protocol.ID, msg []byte, timeout time.Duration) ([]byte, error) {
	s, err := n.host.NewStream(n.ctx, peerID, protocol)
	if err != nil {
		return nil, err
	}
	stream := NewTimeoutStream(s)
	defer func() {
		if err := stream.Close(); err != nil {
			n.logger.Error("could not close stream", zap.Error(err))
		}
	}()
	metricsStreamRequests.Inc()
	metricsStreamRequestsActive.Inc()
	defer metricsStreamRequestsActive.Dec()

	if err := stream.WriteWithTimeout(msg, timeout); err != nil {
		return nil, errors.Wrap(err, "could not write to stream")
	}
	if err := s.CloseWrite(); err != nil {
		return nil, errors.Wrap(err, "could not close write stream")
	}
	res, err := stream.ReadWithTimeout(timeout)
	if err != nil {
		return nil, errors.Wrap(err, "could not read stream msg")
	}
	metricsStreamRequestsSuccess.Inc()
	return res, nil
}

// HandleStream is called at the beginning of stream handlers to create a wrapper stream and read first message
func (n *streamCtrl) HandleStream(stream core.Stream) (*network.Message, network.SyncStream, error) {
	s := NewTimeoutStream(stream)

	msg, err := n.readMsg(s)
	if err != nil {
		return nil, nil, err
	}
	streamID := n.add(s)
	msg.StreamID = streamID

	return msg, s, nil
}

// Respond responds with the given message, streamID is taken from the msg
func (n *streamCtrl) Respond(msg *network.Message) error {
	if msg == nil {
		return errors.New("could not respond with nil message")
	}
	s := n.pop(msg.StreamID)
	if s == nil {
		return errors.Errorf("stream not found: %s", msg.StreamID)
	}
	if err := n.sendMsg(s, msg); err != nil {
		return err
	}
	metricsStreamResponses.Inc()
	if err := s.Close(); err != nil {
		return errors.Wrap(err, "could not close stream")
	}
	return nil
}

func (n *streamCtrl) sendMsg(stream network.SyncStream, msg *network.Message) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(msg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	if err := stream.WriteWithTimeout(msgBytes, n.requestTimeout); err != nil {
		return errors.Wrap(err, "could not write to stream")
	}
	return nil
}

func (n *streamCtrl) readMsg(stream network.SyncStream) (*network.Message, error) {
	resByts, err := stream.ReadWithTimeout(n.requestTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "could not read stream msg")
	}
	resMsg, err := n.fork.DecodeNetworkMsg(resByts)
	if err != nil {
		return nil, errors.Wrap(err, "could not decode stream msg")
	}

	return resMsg, nil
}

// clean removes expired entries
func (n *streamCtrl) clean() {
	n.streamsLock.Lock()
	defer n.streamsLock.Unlock()

	now := time.Now()
	for id, entry := range n.streams {
		if entry.t.Add(cacheTimeout).After(now) {
			delete(n.streams, id)
		}
	}
}

// add adds the stream based on its id
func (n *streamCtrl) add(stream network.SyncStream) string {
	n.streamsLock.Lock()
	defer n.streamsLock.Unlock()

	streamID := stream.ID()
	// core.Stream::ID() is mentioned to be unique (might repeat across restarts)
	// therefore, it should be safe to override. keeping the log for now to watch this
	if _, ok := n.streams[streamID]; ok {
		n.logger.Warn("duplicated stream id", zap.String("streamID", streamID))
	}

	n.streams[streamID] = streamEntry{
		s: stream,
		t: time.Now(),
	}

	return streamID
}

// pop removes adn returns the stream with the given id
func (n *streamCtrl) pop(id string) network.SyncStream {
	n.streamsLock.Lock()
	defer n.streamsLock.Unlock()

	if entry, ok := n.streams[id]; ok {
		delete(n.streams, id)
		return entry.s
	}
	return nil
}
