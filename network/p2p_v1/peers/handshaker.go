package peers

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"time"
)

const (
	// HandshakeProtocol is the protocol.ID used for handshake
	HandshakeProtocol = "/ssv/handshake/0.0.1"

	userAgentKey = "AgentVersion"
)

// HandshakeFilter can be used to filter nodes once we handshaked with them
type HandshakeFilter func(*NodeInfo) (bool, error)

// Handshaker is the interface for handshaking with peers
// it manages the node info protocol
type Handshaker interface {
	Handshake(conn libp2pnetwork.Conn) error
	Handler() libp2pnetwork.StreamHandler
}

type handshaker struct {
	ctx context.Context

	logger *zap.Logger

	filters []HandshakeFilter

	streams streams.StreamController

	idx IdentityIndex
	// for backwards compatibility
	ids *identify.IDService
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(ctx context.Context, logger *zap.Logger, streams streams.StreamController, idx IdentityIndex,
	ids *identify.IDService, filters ...HandshakeFilter) Handshaker {
	h := &handshaker{
		ctx:     ctx,
		logger:  logger,
		streams: streams,
		idx:     idx,
		ids:     ids,
		filters: filters,
	}
	return h
}

// Handler returns the handshake handler
func (h *handshaker) Handler() libp2pnetwork.StreamHandler {
	return func(stream libp2pnetwork.Stream) {
		req, res, done, err := h.streams.HandleStream(stream)
		defer done()
		if err != nil {
			h.logger.Warn("could not read node info msg", zap.Error(err))
			return
		}
		nodeInfo, err := DecodeNodeInfo(req)
		if err != nil {
			h.logger.Warn("could not decode node info msg", zap.Error(err))
			return
		}
		h.logger.Debug("handling handshake request from peer", zap.Any("nodeInfo", nodeInfo))
		if !h.applyFilters(nodeInfo) {
			h.logger.Debug("filtering peer", zap.Any("nodeInfo", nodeInfo))
			return
		}
		if added, err := h.idx.Add(nodeInfo); err != nil {
			h.logger.Warn("could not add node info", zap.Error(err))
			return
		} else if !added {
			h.logger.Warn("nodeInfo was not added", zap.String("id", nodeInfo.ID))
		}

		self, err := h.idx.Self().Encode()
		if err != nil {
			h.logger.Warn("could not marshal self node info", zap.Error(err))
			return
		}

		if err := res(self); err != nil {
			h.logger.Warn("could not send self node info", zap.Error(err))
			return
		}
		//h.logger.Debug("successful handshake", zap.String("id", nodeInfo.ID))
	}
}

// Handshake initiates handshake with the given conn
func (h *handshaker) Handshake(conn libp2pnetwork.Conn) error {
	// check if the peer is known
	//nodeInfo, err := h.idx.NodeInfo(conn.RemotePeer())
	//if err != nil && err != ErrNotFound {
	//	return errors.Wrap(err, "could not read nodeInfo")
	//}
	//if nodeInfo != nil {
	//	return nil
	//}

	pid := conn.RemotePeer()
	nodeInfo, err := h.handshake(pid)
	if err != nil {
		// v0 nodes are not supporting the new protocol
		// fallbacks to user agent
		h.logger.Debug("could not handshake, trying with user agent", zap.String("id", pid.String()), zap.Error(err))
		nodeInfo, err = h.handshakeWithUserAgent(conn)
	}
	if err != nil {
		return errors.Wrap(err, "could not handshake")
	}
	if nodeInfo == nil {
		return errors.New("empty nodeInfo")
	}
	if !h.applyFilters(nodeInfo) {
		h.logger.Debug("filtering peer", zap.Any("nodeInfo", nodeInfo))
		return errors.New("peer was filtered")
	}
	// adding to index
	_, err = h.idx.Add(nodeInfo)
	//if added {
	//	h.logger.Debug("new peer added after handshake", zap.String("id", pid.String()))
	//}
	if err != nil {
		h.logger.Warn("could not add peer to index", zap.String("id", pid.String()))
	}
	return err
}

func (h *handshaker) handshake(id peer.ID) (*NodeInfo, error) {
	data, err := h.idx.Self().Encode()
	if err != nil {
		return nil, err
	}
	resBytes, err := h.streams.Request(id, HandshakeProtocol, data)
	if err != nil {
		return nil, err
	}
	return DecodeNodeInfo(resBytes)
}

func (h *handshaker) handshakeWithUserAgent(conn libp2pnetwork.Conn) (*NodeInfo, error) {
	pid := conn.RemotePeer()
	ctx, cancel := context.WithTimeout(h.ctx, time.Second*10)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, errors.New("identity (user agent) protocol timeout")
	case <-h.ids.IdentifyWait(conn):
	}
	uaRaw, err := h.ids.Host.Peerstore().Get(pid, userAgentKey)
	if err != nil {
		return nil, err
	}
	ua, ok := uaRaw.(string)
	if !ok {
		return nil, errors.New("could not cast ua to string")
	}
	return nodeInfoFromUserAgent(ua, pid.String()), nil
}

func (h *handshaker) applyFilters(nodeInfo *NodeInfo) bool {
	for _, filter := range h.filters {
		ok, err := filter(nodeInfo)
		if err != nil {
			h.logger.Warn("could not filter nodeInfo", zap.Error(err))
			return false
		}
		if !ok {
			h.logger.Debug("filtering peer", zap.Any("nodeInfo", nodeInfo))
			return false
		}
	}
	return true
}

func nodeInfoFromUserAgent(ua string, pid string) *NodeInfo {
	parts := strings.Split(ua, ":")
	if len(parts) < 3 { // too old
		return nil
	}
	idn := new(NodeInfo)
	idn.ID = pid
	// TODO: extract v0 to constant
	idn.ForkV = "v0"
	idn.Metadata = make(map[string]string)
	idn.Metadata[nodeVersionKey] = parts[1]
	if len(parts) > 3 { // operator
		idn.OperatorID = parts[3]
	}
	return idn
}
