package peers

import (
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"go.uber.org/zap"
)

const (
	// HandshakeProtocol is the protocol.ID used for handshake
	HandshakeProtocol = "/ssv/handshake/0.0.1"
)

// Handshaker is the interface for handshaking with peers
type Handshaker interface {
	Handshake(conn libp2pnetwork.Conn) error
	Handler() libp2pnetwork.StreamHandler
}

type handshaker struct {
	logger *zap.Logger

	streams streams.StreamController

	idx IdentityIndex
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(logger *zap.Logger, streams streams.StreamController, idx IdentityIndex) Handshaker {
	h := &handshaker{
		logger:  logger,
		streams: streams,
		idx:     idx,
	}
	return h
}

// Handler returns the handshake handler
func (h *handshaker) Handler() libp2pnetwork.StreamHandler {
	return func(stream libp2pnetwork.Stream) {
		req, res, done, err := h.streams.HandleStream(stream)
		defer done()
		if err != nil {
			h.logger.Warn("could not read identity msg", zap.Error(err))
			return
		}
		identity, err := DecodeIdentity(req)
		if err != nil {
			h.logger.Warn("could not decode identity msg", zap.Error(err))
			return
		}
		h.logger.Debug("handling handshake request from peer", zap.Any("identity", identity))
		if added, err := h.idx.Add(identity); err != nil {
			h.logger.Warn("could not add identity identity", zap.Error(err))
			return
		} else if !added {
			h.logger.Warn("identity was not added", zap.String("id", identity.ID))
		}

		self, err := h.idx.Self().Encode()
		if err != nil {
			h.logger.Warn("could not marshal self identity", zap.Error(err))
			return
		}

		if err := res(self); err != nil {
			h.logger.Warn("could not send self identity", zap.Error(err))
			return
		}
		h.logger.Debug("successful handshake", zap.String("id", identity.ID))
	}
}

// Handshake initiates handshake with the given conn
// TODO: extract streams logic into streams
func (h *handshaker) Handshake(conn libp2pnetwork.Conn) error {
	identity, err := h.idx.Identity(conn.RemotePeer())
	if err != nil && err != ErrNotFound {
		return err
	}
	if identity != nil {
		return nil
	}
	data, err := h.idx.Self().Encode()
	if err != nil {
		return err
	}
	pid := conn.RemotePeer()
	//h.logger.Debug("handshaking peer", zap.String("id", pid.String()))
	resBytes, err := h.streams.Request(pid, HandshakeProtocol, data)
	if err != nil {
		return err
	}
	res, err := DecodeIdentity(resBytes)
	if err != nil {
		return err
	}
	added, err := h.idx.Add(res)
	if added {
		h.logger.Debug("handshaked peer", zap.String("id", pid.String()))
	}
	if err != nil {
		h.logger.Warn("could not add peer to index", zap.String("id", pid.String()))
	}
	return err
}
