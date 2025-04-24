package connections

import (
	"context"
	"encoding/hex"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

// errPeerWasFiltered is thrown when a peer is filtered during handshake
var errPeerWasFiltered = errors.New("peer was filtered during handshake")

// errConsumingMessage is thrown when we сan't consume(parse) message: data is broken or incoming msg is from node with different Permissioned mode
var errConsumingMessage = errors.New("error consuming message")

// HandshakeFilter can be used to filter nodes once we handshaked with them
type HandshakeFilter func(senderID peer.ID, sni *records.NodeInfo) error

// SubnetsProvider returns the subnets of or node
type SubnetsProvider func() commons.Subnets

// Handshaker is the interface for handshaking with peers.
// it uses node info protocol to exchange information with other nodes and decide whether we want to connect.
//
// NOTE: due to compatibility with v0,
// we accept nodes with user agent as a fallback when the new protocol is not supported.
type Handshaker interface {
	Handshake(logger *zap.Logger, conn libp2pnetwork.Conn) error
	Handler(logger *zap.Logger) libp2pnetwork.StreamHandler
}

type handshaker struct {
	ctx context.Context

	filters func() []HandshakeFilter

	streams    streams.StreamController
	nodeInfos  peers.NodeInfoIndex
	peerInfos  peers.PeerInfoIndex
	connIdx    peers.ConnectionIndex
	subnetsIdx peers.SubnetsIndex
	ids        identify.IDService
	net        libp2pnetwork.Network

	domainType      spectypes.DomainType
	subnetsProvider SubnetsProvider
}

// HandshakerCfg is the configuration for creating an handshaker instance
type HandshakerCfg struct {
	Network         libp2pnetwork.Network
	Streams         streams.StreamController
	NodeInfos       peers.NodeInfoIndex
	PeerInfos       peers.PeerInfoIndex
	ConnIdx         peers.ConnectionIndex
	SubnetsIdx      peers.SubnetsIndex
	IDService       identify.IDService
	OperatorSigner  keys.OperatorSigner
	DomainType      spectypes.DomainType
	SubnetsProvider SubnetsProvider
}

// NewHandshaker creates a new instance of handshaker
func NewHandshaker(ctx context.Context, cfg *HandshakerCfg, filters func() []HandshakeFilter) Handshaker {
	h := &handshaker{
		ctx:             ctx,
		streams:         cfg.Streams,
		nodeInfos:       cfg.NodeInfos,
		connIdx:         cfg.ConnIdx,
		subnetsIdx:      cfg.SubnetsIdx,
		ids:             cfg.IDService,
		filters:         filters,
		peerInfos:       cfg.PeerInfos,
		subnetsProvider: cfg.SubnetsProvider,
		domainType:      cfg.DomainType,
		net:             cfg.Network,
	}
	return h
}

// Handler returns the handshake handler
func (h *handshaker) Handler(logger *zap.Logger) libp2pnetwork.StreamHandler {
	handleHandshake := func(logger *zap.Logger, h *handshaker, stream libp2pnetwork.Stream) error {
		pid := stream.Conn().RemotePeer()
		request, respond, done, err := h.streams.HandleStream(logger, stream)
		defer done()
		if err != nil {
			return err
		}

		// Check if the node requires permissioned peers.
		nodeInfo := &records.NodeInfo{}
		err = nodeInfo.Consume(request)
		if err != nil {
			return errors.Wrap(err, "could not consume node info request")
		}

		// Respond with our own NodeInfo.
		self, err := h.sealedNodeRecord()
		if err != nil {
			return errors.Wrap(err, "could not seal self node info")
		}

		if err := respond(self); err != nil {
			return errors.Wrap(err, "could not send self node info")
		}

		err = h.verifyTheirNodeInfo(logger, pid, nodeInfo)
		if err != nil {
			return errors.Wrap(err, "failed verifying their node info")
		}
		return nil
	}

	return func(stream libp2pnetwork.Stream) {
		pid := stream.Conn().RemotePeer()
		logger := logger.With(fields.PeerID(pid))

		// Update PeerInfo with the result of this handshake.
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = errors.Errorf("panic: %v", r)
			}
			h.updatePeerInfo(pid, err)
		}()

		// Handle the handshake request.
		err = handleHandshake(logger, h, stream)
	}
}

func (h *handshaker) verifyTheirNodeInfo(logger *zap.Logger, sender peer.ID, ni *records.NodeInfo) error {
	h.updateNodeSubnets(logger, sender, ni.GetNodeInfo())

	if err := h.applyFilters(sender, ni); err != nil {
		return err
	}

	h.nodeInfos.SetNodeInfo(sender, ni.GetNodeInfo())

	logger.Info("Verified handshake nodeinfo",
		fields.PeerID(sender),
		zap.Any("metadata", ni.GetNodeInfo().Metadata),
		zap.String("networkID", ni.GetNodeInfo().NetworkID),
	)

	return nil
}

// Handshake initiates handshake with the given conn
func (h *handshaker) Handshake(logger *zap.Logger, conn libp2pnetwork.Conn) (err error) {
	pid := conn.RemotePeer()
	var nodeInfo *records.NodeInfo

	// Update PeerInfo with the result of this handshake.
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf("panic: %v", r)
		}
		h.updatePeerInfo(pid, err)
	}()

	nodeInfo, err = h.requestNodeInfo(logger, conn)
	if err != nil {
		err = errors.Wrap(err, "failed requesting node info")
		return
	}

	err = h.verifyTheirNodeInfo(logger, pid, nodeInfo)
	if err != nil {
		err = errors.Wrap(err, "failed verifying their node info")
		return
	}
	return
}

func (h *handshaker) updatePeerInfo(pid peer.ID, handshakeErr error) {
	h.peerInfos.UpdatePeerInfo(pid, func(info *peers.PeerInfo) {
		info.LastHandshake = time.Now()
		info.LastHandshakeError = handshakeErr
	})
}

// updateNodeSubnets tries to update the subnets of the given peer
func (h *handshaker) updateNodeSubnets(logger *zap.Logger, pid peer.ID, ni *records.NodeInfo) {
	if ni.Metadata != nil {
		subnets, err := commons.FromString(ni.Metadata.Subnets)
		if err == nil {
			updated := h.subnetsIdx.UpdatePeerSubnets(pid, subnets)
			if updated {
				logger.Debug("[handshake] peer subnets were updated", fields.PeerID(pid),
					zap.String("subnets", subnets.String()))
			}
		}
	}
}

func (h *handshaker) requestNodeInfo(logger *zap.Logger, conn libp2pnetwork.Conn) (*records.NodeInfo, error) {
	data, err := h.sealedNodeRecord()

	if err != nil {
		return nil, err
	}

	resBytes, err := h.streams.Request(logger, conn.RemotePeer(), peers.NodeInfoProtocol, data)
	if err != nil {
		return nil, err
	}

	nodeInfo := &records.NodeInfo{}

	if err := nodeInfo.Consume(resBytes); err != nil {
		return nil, errors.Wrap(errConsumingMessage, err.Error())
	}
	return nodeInfo, nil
}

func (h *handshaker) applyFilters(sender peer.ID, ni *records.NodeInfo) error {
	fltrs := h.filters()
	for i := range fltrs {
		err := fltrs[i](sender, ni)
		if err != nil {
			return errors.Wrap(errPeerWasFiltered, err.Error())
		}
	}

	return nil
}

func (h *handshaker) sealedNodeRecord() ([]byte, error) {
	// Update DomainType.
	h.nodeInfos.UpdateSelfRecord(func(self *records.NodeInfo) *records.NodeInfo {
		self.NetworkID = "0x" + hex.EncodeToString(h.domainType[:])
		return self
	})

	return h.nodeInfos.SelfSealed()
}
