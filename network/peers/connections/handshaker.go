package connections

import (
	"context"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/network/streams"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

const (
	// userAgentKey is the key used by libp2p to save user agent
	userAgentKey = "AgentVersion"
)

// errHandshakeInProcess is thrown when and handshake process for that peer is already running
var errHandshakeInProcess = errors.New("handshake already in process")

// errPeerWasFiltered is thrown when a peer is filtered during handshake
var errPeerWasFiltered = errors.New("peer was filtered during handshake")

// errUnknownUserAgent is thrown when a peer has an unknown user agent
var errUnknownUserAgent = errors.New("user agent is unknown")

// HandshakeFilter can be used to filter nodes once we handshaked with them
type HandshakeFilter func(info *records.NodeInfo) (bool, error)

// SubnetsProvider returns the subnets of or node
type SubnetsProvider func() records.Subnets

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

	streams     streams.StreamController
	nodeInfoIdx peers.NodeInfoIndex
	states      peers.NodeStates
	connIdx     peers.ConnectionIndex
	subnetsIdx  peers.SubnetsIndex
	ids         *identify.IDService

	pending *sync.Map

	subnetsProvider SubnetsProvider
}

// HandshakerCfg is the configuration for creating an handshaker instance
type HandshakerCfg struct {
	Logger          *zap.Logger
	Streams         streams.StreamController
	NodeInfoIdx     peers.NodeInfoIndex
	States          peers.NodeStates
	ConnIdx         peers.ConnectionIndex
	SubnetsIdx      peers.SubnetsIndex
	IDService       *identify.IDService
	SubnetsProvider SubnetsProvider
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(ctx context.Context, cfg *HandshakerCfg, filters ...HandshakeFilter) Handshaker {
	h := &handshaker{
		ctx:             ctx,
		logger:          cfg.Logger.With(zap.String("where", "Handshaker")),
		streams:         cfg.Streams,
		nodeInfoIdx:     cfg.NodeInfoIdx,
		connIdx:         cfg.ConnIdx,
		subnetsIdx:      cfg.SubnetsIdx,
		ids:             cfg.IDService,
		filters:         filters,
		pending:         &sync.Map{},
		states:          cfg.States,
		subnetsProvider: cfg.SubnetsProvider,
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
		// update node's subnets if applicable
		h.updateNodeSubnets(pid, &ni)
		//h.logger.Debug("handling handshake request from peer", zap.Any("info", ni))
		if !h.applyFilters(&ni) {
			//h.logger.Debug("filtering peer", zap.Any("info", ni))
			return
		}
		if _, err := h.nodeInfoIdx.AddNodeInfo(pid, &ni); err != nil {
			h.logger.Warn("could not add node info", zap.Error(err))
			return
		}
		self, err := h.nodeInfoIdx.SelfSealed()
		if err != nil {
			h.logger.Warn("could not seal self node info", zap.Error(err))
			return
		}
		if err := res(self); err != nil {
			h.logger.Warn("could not send self node info", zap.Error(err))
			return
		}
		// if we reached limit, check that the peer has at least 5 shared subnets
		// TODO: dynamic/configurable value TBD
		if h.connIdx.Limit(stream.Conn().Stat().Direction) {
			ok, _ := SharedSubnetsFilter(h.subnetsProvider, 5)(&ni)
			if !ok {
				h.logger.Warn("reached peers limit", zap.Error(err))
				return
			}
		} /* else if ok, _ := SharedSubnetsFilter(h.subnetsProvider, 1)(ni); !ok {
			return errors.New("ignoring peer w/o shared subnets")
		}*/
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
	// check if the peer is known
	ni, err := h.nodeInfoIdx.GetNodeInfo(pid)
	if err != nil && err != peers.ErrNotFound {
		return errors.Wrap(err, "could not read identity")
	}
	if ni != nil {
		switch h.states.State(pid) {
		case peers.StateIndexing:
			return errHandshakeInProcess
		case peers.StatePruned:
			return errors.Errorf("pruned peer [%s]", pid.String())
		case peers.StateReady:
			return nil
		default: // unknown > continue the flow
		}
	}
	if err := h.preHandshake(conn); err != nil {
		return errors.Wrap(err, "could not perform pre-handshake")
	}
	ni, err = h.nodeInfoFromStream(conn)
	if err != nil {
		// fallbacks to user agent
		ni, err = h.nodeInfoFromUserAgent(conn)
		if err != nil {
			return err
		}
	}
	if ni == nil {
		return errors.New("empty node info")
	}
	// update node's subnets if applicable
	h.updateNodeSubnets(pid, ni)

	if !h.applyFilters(ni) {
		return errPeerWasFiltered
	}
	logger := h.logger.With(zap.String("otherPeer", pid.String()), zap.Any("info", ni))
	// adding to index
	_, err = h.nodeInfoIdx.AddNodeInfo(pid, ni)
	if err != nil {
		logger.Warn("could not add peer to index", zap.Error(err))
		return err
	}

	// if we reached limit, check that the peer has at least 5 shared subnets
	// TODO: dynamic/configurable value TBD
	if h.connIdx.Limit(conn.Stat().Direction) {
		ok, _ := SharedSubnetsFilter(h.subnetsProvider, 5)(ni)
		if !ok {
			return errors.New("reached peers limit")
		}
	} /* else if ok, _ := SharedSubnetsFilter(h.subnetsProvider, 1)(ni); !ok {
		return errors.New("ignoring peer w/o shared subnets")
	}*/

	return nil
}

// updateNodeSubnets tries to update the subnets of the given peer
func (h *handshaker) updateNodeSubnets(pid peer.ID, ni *records.NodeInfo) {
	if ni.Metadata != nil {
		subnets, err := records.Subnets{}.FromString(ni.Metadata.Subnets)
		if err == nil && len(subnets) > 0 {
			updated := h.subnetsIdx.UpdatePeerSubnets(pid, subnets)
			h.logger.Debug("handshaked peer subnets", zap.String("peerID", pid.String()),
				zap.String("subnets", subnets.String()),
				zap.Bool("updated", updated))
		}
	}
}

func (h *handshaker) nodeInfoFromStream(conn libp2pnetwork.Conn) (*records.NodeInfo, error) {
	res, err := h.ids.Host.Peerstore().FirstSupportedProtocol(conn.RemotePeer(), peers.NodeInfoProtocol)
	if err != nil {
		return nil, errors.Wrapf(err, "could not check supported protocols of peer %s",
			conn.RemotePeer().String())
	}
	data, err := h.nodeInfoIdx.SelfSealed()
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, errors.Errorf("peer [%s] doesn't supports handshake protocol", conn.RemotePeer().String())
	}
	resBytes, err := h.streams.Request(conn.RemotePeer(), peers.NodeInfoProtocol, data)
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
		h.logger.Debug("user agent is unknown", zap.String("ua", ua))
		return nil, errUnknownUserAgent
	}
	// TODO: don't assume network is the same
	ni := records.NewNodeInfo(forksprotocol.V0ForkVersion, h.nodeInfoIdx.Self().NetworkID)
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
			//h.logger.Warn("could not filter peer", zap.Error(err), zap.Any("nodeInfo", nodeInfo))
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}
