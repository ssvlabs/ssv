package peers

import (
	"context"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

const (
	// NodeInfoProtocol is the protocol.ID used for handshake
	NodeInfoProtocol = "/ssv/info/0.0.1"
	// userAgentKey is the key used by libp2p to save user agent
	userAgentKey = "AgentVersion"
)

// errHandshakeInProcess is thrown when and handshake process for that peer is already running
var errHandshakeInProcess = errors.New("handshake already in process")

// ErrAtPeersLimit is thrown when we reached peers limit
var ErrAtPeersLimit = errors.New("peers limit was reached")

// HandshakeFilter can be used to filter nodes once we handshaked with them
type HandshakeFilter func(info *records.NodeInfo) (bool, error)

// Handshaker is the interface for handshaking with peers.
// it uses node info protocol to exchange information with other nodes and decide whether we want to connect.
//
// NOTE: due to compatibility with v0,
// we accept nodes with user agent as a fallback when the new protocol is not supported.
type Handshaker interface {
	Handshake(conn libp2pnetwork.Conn) error
	Handler() libp2pnetwork.StreamHandler
}

type handshaker struct {
	ctx context.Context

	logger *zap.Logger

	filters []HandshakeFilter

	streams streams.StreamController

	infoStore NodeInfoStore

	connIdx ConnectionIndex
	// for backwards compatibility
	ids *identify.IDService

	pending *sync.Map
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(ctx context.Context, logger *zap.Logger, streams streams.StreamController, idx NodeInfoStore, connIdx ConnectionIndex, ids *identify.IDService, filters ...HandshakeFilter) Handshaker {
	h := &handshaker{
		ctx:       ctx,
		logger:    logger,
		streams:   streams,
		infoStore: idx,
		connIdx:   connIdx,
		ids:       ids,
		filters:   filters,
		pending:   &sync.Map{},
	}
	return h
}

// Handler returns the handshake handler
func (h *handshaker) Handler() libp2pnetwork.StreamHandler {
	return func(stream libp2pnetwork.Stream) {
		// start by marking the peer as pending
		pid := stream.Conn().RemotePeer()
		h.pending.Store(pid.String(), true)
		defer h.pending.Delete(pid.String())

		req, res, done, err := h.streams.HandleStream(stream)
		defer done()
		if err != nil {
			h.logger.Warn("could not read node info msg", zap.Error(err))
			return
		}

		var ni records.NodeInfo
		err = ni.Consume(req)
		if err != nil {
			h.logger.Warn("could not consume node info request", zap.Error(err))
			return
		}
		//h.logger.Debug("handling handshake request from peer", zap.Any("info", ni))
		if !h.applyFilters(&ni) {
			h.logger.Debug("filtering peer", zap.Any("info", ni))
			return
		}
		if added, err := h.infoStore.Add(pid, &ni); err != nil {
			h.logger.Warn("could not add node info", zap.Error(err))
			return
		} else if !added {
			h.logger.Warn("node info was not added", zap.String("id", pid.String()))
		}
		self, err := h.infoStore.SelfSealed()
		if err != nil {
			h.logger.Error("could not seal self node info", zap.Error(err))
			return
		}
		if err := res(self); err != nil {
			h.logger.Warn("could not send self node info", zap.Error(err))
			return
		}
	}
}

// preHandshake makes sure that we didn't reach peers limit and have exchanged framework information (libp2p)
// with the peer on the other side of the connection.
// it should enable us to know the supported protocols of peers we connect to
func (h *handshaker) preHandshake(conn libp2pnetwork.Conn) error {
	ctx, cancel := context.WithTimeout(h.ctx, time.Second*15)
	defer cancel()
	select {
	case <-ctx.Done():
		return errors.New("identity protocol (libp2p) timeout")
	case <-h.ids.IdentifyWait(conn):
	}
	return nil
}

// Handshake initiates handshake with the given conn
func (h *handshaker) Handshake(conn libp2pnetwork.Conn) error {
	pid := conn.RemotePeer()
	if _, loaded := h.pending.LoadOrStore(pid.String(), true); loaded {
		return errHandshakeInProcess
	}
	defer h.pending.Delete(pid.String())

	// first, check that we didn't reach peers limit
	if h.connIdx.Limit(conn.Stat().Direction) {
		h.logger.Debug("reached peers limit")
		return nil
	}
	// check if the peer is known
	ni, err := h.infoStore.NodeInfo(pid)
	if err != nil && err != ErrNotFound {
		return errors.Wrap(err, "could not read identity")
	}
	if ni != nil {
		switch h.infoStore.State(pid) {
		case StateIndexing:
			return errHandshakeInProcess
		case StatePruned:
			return errors.Errorf("pruned peer [%s]", pid.String())
		case StateUnknown:
			// continue the flow
		default: // ready
			return nil
		}
	}

	if err := h.preHandshake(conn); err != nil {
		return errors.Wrap(err, "could not perform pre-handshake")
	}
	ni, err = h.nodeInfoFromStream(conn)
	if err != nil {
		// v0 nodes are not supporting the new protocol
		// fallbacks to user agent
		ni, err = h.nodeInfoFromUserAgent(conn)
	}
	if err != nil {
		return errors.Wrapf(err, "could not handshake with peer [%s]", pid.String())
	}
	if ni == nil {
		return errors.New("empty identity")
	}
	if !h.applyFilters(ni) {
		return errors.Errorf("peer [%s] was filtered during handshake", pid.String())
	}
	// adding to index
	added, err := h.infoStore.Add(pid, ni)
	if added {
		//h.logger.Debug("new peer added after handshake", zap.String("id", pid.String()), zap.Any("info", ni))
		return nil
	}
	if err != nil {
		h.logger.Warn("could not add peer to index", zap.String("id", pid.String()), zap.Any("info", ni))
	}
	return err
}

func (h *handshaker) nodeInfoFromStream(conn libp2pnetwork.Conn) (*records.NodeInfo, error) {
	res, err := h.ids.Host.Peerstore().FirstSupportedProtocol(conn.RemotePeer(), NodeInfoProtocol)
	if err != nil {
		return nil, errors.Wrapf(err, "could not check supported protocols of peer %s",
			conn.RemotePeer().String())
	}
	data, err := h.infoStore.SelfSealed()
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, errors.Errorf("peer [%s] doesn't supports handshake protocol", conn.RemotePeer().String())
	}
	resBytes, err := h.streams.Request(conn.RemotePeer(), NodeInfoProtocol, data)
	if err != nil {
		return nil, err
	}
	var ni records.NodeInfo
	err = ni.Consume(resBytes)
	if err != nil {
		return nil, err
	}
	return &ni, nil
}

func (h *handshaker) nodeInfoFromUserAgent(conn libp2pnetwork.Conn) (*records.NodeInfo, error) {
	pid := conn.RemotePeer()
	uaRaw, err := h.ids.Host.Peerstore().Get(pid, userAgentKey)
	if err != nil {
		if err == peerstore.ErrNotFound {
			// if user agent wasn't found, retry libp2p identify after 100ms
			// TODO: find the root cause of this issue
			time.Sleep(time.Millisecond * 100)
			if err := h.preHandshake(conn); err != nil {
				return nil, err
			}
			uaRaw, err = h.ids.Host.Peerstore().Get(pid, userAgentKey)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	ua, ok := uaRaw.(string)
	if !ok {
		return nil, errors.New("could not cast ua to string")
	}
	parts := strings.Split(ua, ":")
	if len(parts) < 2 { // too old or unknown
		return nil, errors.Errorf("user agent is unknown: %s", ua)
	}
	// TODO: don't assume network is the same
	ni := records.NewNodeInfo(forksprotocol.V0ForkVersion, h.infoStore.Self().NetworkID)
	ni.Metadata = &records.NodeMetadata{
		NodeVersion: parts[1],
	}
	// extract operator id if exist
	if len(parts) > 3 {
		ni.Metadata.OperatorID = parts[3]
	}
	return ni, nil
}

func (h *handshaker) applyFilters(nodeInfo *records.NodeInfo) bool {
	for _, filter := range h.filters {
		ok, err := filter(nodeInfo)
		if err != nil {
			h.logger.Warn("could not filter identity", zap.Error(err), zap.Any("identity", nodeInfo))
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}

// ForkVersionFilter determines whether we will connect to the given node by the fork version
func ForkVersionFilter(forkVersion func() forksprotocol.ForkVersion) HandshakeFilter {
	return func(ni *records.NodeInfo) (bool, error) {
		version := forkVersion()
		if version != ni.ForkVersion {
			return false, errors.Errorf("fork version '%s' instead of '%s'", ni.ForkVersion.String(), version)
		}
		return true, nil
	}
}

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkID string) HandshakeFilter {
	return func(ni *records.NodeInfo) (bool, error) {
		if networkID != ni.NetworkID {
			return false, errors.Errorf("networkID '%s' instead of '%s'", ni.NetworkID, networkID)
		}
		return true, nil
	}
}
