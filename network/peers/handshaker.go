package peers

import (
	"context"
	"github.com/bloxapp/ssv/network/streams"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

const (
	// HandshakeProtocol is the protocol.ID used for handshake
	HandshakeProtocol = "/ssv/handshake/0.0.1"

	userAgentKey = "AgentVersion"
)

var ErrHandshakeInProcess = errors.New("handshake already in process")

// HandshakeFilter can be used to filter nodes once we handshaked with them
type HandshakeFilter func(*Identity) (bool, error)

// Handshaker is the interface for handshaking with peers
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

	pending *sync.Map
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(ctx context.Context, logger *zap.Logger, streams streams.StreamController, idx IdentityIndex, ids *identify.IDService, filters ...HandshakeFilter) Handshaker {
	h := &handshaker{
		ctx:     ctx,
		logger:  logger,
		streams: streams,
		idx:     idx,
		ids:     ids,
		filters: filters,
		pending: &sync.Map{},
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
			h.logger.Warn("could not read identity msg", zap.Error(err))
			return
		}

		identity, err := DecodeIdentity(req)
		if err != nil {
			h.logger.Warn("could not decode identity msg", zap.Error(err))
			return
		}
		h.logger.Debug("handling handshake request from peer", zap.Any("identity", identity))
		if !h.applyFilters(identity) {
			h.logger.Debug("filtering peer", zap.Any("identity", identity))
			return
		}
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
		//h.logger.Debug("successful handshake", zap.String("id", identity.ID))
	}
}

// Handshake initiates handshake with the given conn
func (h *handshaker) Handshake(conn libp2pnetwork.Conn) error {
	pid := conn.RemotePeer()
	if _, loaded := h.pending.LoadOrStore(pid.String(), true); loaded {
		return ErrHandshakeInProcess
	}
	defer h.pending.Delete(pid.String())

	// check if the peer is known
	idn, err := h.idx.Identity(pid)
	if err != nil && err != ErrNotFound {
		return errors.Wrap(err, "could not read identity")
	}
	if idn != nil {
		return nil
	}

	idn, err = h.sendInfo(conn)
	if err != nil {
		// v0 nodes are not supporting the new protocol
		// fallbacks to user agent
		h.logger.Debug("could not handshake, trying with user agent", zap.String("id", pid.String()), zap.Error(err))
		idn, err = h.handshakeWithUserAgent(conn)
	}
	if err != nil {
		return errors.Wrap(err, "could not handshake")
	}
	if idn == nil {
		return errors.New("empty identity")
	}
	if !h.applyFilters(idn) {
		h.logger.Debug("filtering peer", zap.Any("identity", idn))
		return errors.New("peer was filtered")
	}
	// adding to index
	added, err := h.idx.Add(idn)
	if added {
		h.logger.Debug("new peer added after handshake", zap.String("id", pid.String()), zap.Any("identity", idn))
	}
	if err != nil {
		h.logger.Warn("could not add peer to index", zap.String("id", pid.String()))
	}
	return err
}

func (h *handshaker) sendInfo(conn libp2pnetwork.Conn) (*Identity, error) {
	data, err := h.idx.Self().Encode()
	if err != nil {
		return nil, err
	}
	res, err := h.ids.Host.Peerstore().FirstSupportedProtocol(conn.RemotePeer(), HandshakeProtocol)
	if err != nil {
		return nil, errors.Wrapf(err, "could not check supported protocols of peer %s",
			conn.RemotePeer().String())
	}
	if len(res) == 0 {
		return nil, errors.Errorf("peer %s doesn't supported handshake protocol", conn.RemotePeer().String())
	}
	resBytes, err := h.streams.Request(conn.RemotePeer(), HandshakeProtocol, data)
	if err != nil {
		return nil, err
	}
	return DecodeIdentity(resBytes)
}

func (h *handshaker) handshakeWithUserAgent(conn libp2pnetwork.Conn) (*Identity, error) {
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
	return identityFromUserAgent(ua, pid.String()), nil
}

func (h *handshaker) applyFilters(identity *Identity) bool {
	for _, filter := range h.filters {
		ok, err := filter(identity)
		if err != nil {
			h.logger.Warn("could not filter identity", zap.Error(err), zap.Any("identity", identity))
			return false
		}
		if !ok {
			h.logger.Debug("filtering peer", zap.Any("identity", identity))
			return false
		}
	}
	return true
}

// ForkVersionFilter determines whether we will connect to the given node by the fork version
func ForkVersionFilter(forkVersion forksprotocol.ForkVersion) HandshakeFilter {
	fv := string(forkVersion)
	return func(identity *Identity) (bool, error) {
		if fv != identity.ForkV {
			return false, errors.Errorf("fork version '%s' instead of '%s'", identity.ForkV, fv)
		}
		return true, nil
	}
}

func identityFromUserAgent(ua string, pid string) *Identity {
	parts := strings.Split(ua, ":")
	if len(parts) < 2 { // too old
		return nil
	}
	idn := new(Identity)
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
