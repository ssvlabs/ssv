package node

import (
	"errors"
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/hprobe"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Node struct {
	listenAddresses []string

	network    p2pNetwork
	peersIndex peersIndex
	topicIndex topicIndex

	// this node's own operator identity, as distinct from the p2p identity above
	operatorDataStore operatorDataStore

	healthProber             *hprobe.HealthProber
	clComponentName          string
	elComponentName          string
	eventSyncerComponentName string
}

func NewNode(
	listenAddresses []string,
	peersIndex peersIndex,
	network p2pNetwork,
	topicIndex topicIndex,
	operatorDataStore operatorDataStore,
	healthProber *hprobe.HealthProber,
	clComponentName string,
	elComponentName string,
	eventSyncerComponentName string,
) *Node {
	return &Node{
		listenAddresses:          listenAddresses,
		peersIndex:               peersIndex,
		topicIndex:               topicIndex,
		network:                  network,
		operatorDataStore:        operatorDataStore,
		healthProber:             healthProber,
		clComponentName:          clComponentName,
		elComponentName:          elComponentName,
		eventSyncerComponentName: eventSyncerComponentName,
	}
}

func (h *Node) Identity(w http.ResponseWriter, r *http.Request) error {
	nodeInfo := h.peersIndex.Self()
	resp := identityJSON{
		PeerID: h.network.LocalPeer(),
	}
	if nodeInfo != nil {
		resp.NetworkID = nodeInfo.NetworkID
		// invariant: setupPeerServices initializes self.Metadata at startup, so on
		// the live path nodeInfo.Metadata is always non-nil. The guard defends
		// against a future UpdateSelfRecord caller that returns a NodeInfo without
		// a Metadata block — cheap insurance and mirrors the peers-handler shape.
		if nodeInfo.Metadata != nil {
			resp.Subnets = nodeInfo.Metadata.Subnets
			resp.Version = nodeInfo.Metadata.NodeVersion
		}
	}
	for _, addr := range h.network.ListenAddresses() {
		resp.Addresses = append(resp.Addresses, addr.String())
	}
	// The operator public key is set from config at startup, so it identifies the node
	// from first boot. The id and owner address arrive with this operator's registration
	// event, so they are only reported once that has been synced - reporting id 0 and the
	// zero address before then would read as real values.
	if od := h.operatorDataStore.GetOperatorData(); od != nil {
		resp.OperatorPublicKey = od.PublicKey
		if h.operatorDataStore.OperatorIDReady() {
			resp.OperatorID = od.ID
			resp.OwnerAddress = od.OwnerAddress.String()
		}
	}
	return api.Render(w, r, resp)
}

func (h *Node) Peers(w http.ResponseWriter, r *http.Request) error {
	peers := h.network.Peers()
	resp := h.peers(peers)
	return api.Render(w, r, resp)
}

func (h *Node) Topics(w http.ResponseWriter, r *http.Request) error {
	byTopic := h.topicIndex.PeersByTopic()
	peers := h.network.Peers()
	resp := AllPeersAndTopicsJSON{
		AllPeers:     peers,
		PeersByTopic: make([]topicIndexJSON, 0, len(byTopic)),
	}
	for topic, peers := range byTopic {
		resp.PeersByTopic = append(resp.PeersByTopic, topicIndexJSON{TopicName: topic, Peers: peers})
	}

	return api.Render(w, r, resp)
}

func (h *Node) Health(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var resp healthCheckJSON

	// Retrieve P2P listen addresses.
	resp.Advanced.ListenAddresses = h.listenAddresses

	// Count peers and connections.
	peers := h.network.Peers()
	for _, p := range h.peers(peers) {
		if p.Connectedness == network.Connected.String() {
			resp.Advanced.Peers++
		}
		for _, conn := range p.Connections {
			if conn.Direction == network.DirInbound.String() {
				resp.Advanced.InboundConns++
			} else {
				resp.Advanced.OutboundConns++
			}
		}
	}

	// Report whether P2P is healthy.
	if resp.Advanced.Peers == 0 {
		resp.P2P = healthStatus{errors.New("no peers are connected")}
	} else if resp.Advanced.Peers < healthyPeerCount {
		resp.P2P = healthStatus{errors.New("not enough connected peers")}
	} else if resp.Advanced.InboundConns < healthyInbounds {
		resp.P2P = healthStatus{errors.New("not enough inbound connections, port is likely not reachable")}
	}

	// Check the health of Ethereum nodes and EventSyncer.
	resp.BeaconNode = healthStatus{h.healthProber.Probe(ctx, h.clComponentName)}
	resp.ExecutionNode = healthStatus{h.healthProber.Probe(ctx, h.elComponentName)}
	resp.EventSyncer = healthStatus{h.healthProber.Probe(ctx, h.eventSyncerComponentName)}

	return api.Render(w, r, resp)
}

func (h *Node) peers(peers []peer.ID) []peerJSON {
	resp := make([]peerJSON, len(peers))
	for i, id := range peers {
		subnets, _ := h.peersIndex.GetPeerSubnets(id)

		resp[i] = peerJSON{
			ID:            id,
			Connectedness: h.network.Connectedness(id).String(),
			SubnetsHex:    subnets.StringHex(),
		}

		for _, addr := range h.network.Peerstore().Addrs(id) {
			resp[i].Addresses = append(resp[i].Addresses, addr.String())
		}

		conns := h.network.ConnsToPeer(id)
		for _, conn := range conns {
			resp[i].Connections = append(resp[i].Connections, connectionJSON{
				Address:   conn.RemoteMultiaddr().String(),
				Direction: conn.Stat().Direction.String(),
			})
		}

		nodeInfo := h.peersIndex.NodeInfo(id)
		if nodeInfo == nil || nodeInfo.Metadata == nil {
			// Metadata can be nil if the peer sent a NodeInfo envelope without a
			// metadata block; we reject such peers at handshake time
			// (verifyTheirNodeInfo), but historical entries from before that fix
			// — or from a future reader path — should not crash this endpoint.
			continue
		}
		resp[i].Version = nodeInfo.Metadata.NodeVersion
	}
	return resp
}

type p2pNetwork interface {
	// LocalPeer returns the local peer associated with this network
	LocalPeer() peer.ID

	// ListenAddresses returns a list of addresses at which this network listens.
	ListenAddresses() []ma.Multiaddr

	// Peers returns the peers connected
	Peers() []peer.ID

	// Connectedness returns a state signaling connection capabilities
	Connectedness(peer.ID) network.Connectedness

	// Peerstore returns the internal peerstore
	// This is useful to tell the dialer about a new address for a peer.
	// Or use one of the public keys found out over the network.
	Peerstore() peerstore.Peerstore

	// ConnsToPeer returns the connections in this Network for given peer.
	ConnsToPeer(p peer.ID) []network.Conn
}

type peersIndex interface {
	// Self returns the current node info
	Self() *records.NodeInfo

	// NodeInfo returns the NodeInfo of the given peers, or nil if not found.
	NodeInfo(id peer.ID) *records.NodeInfo

	// GetPeerSubnets returns subnets of the given peer and whether it was found
	GetPeerSubnets(id peer.ID) (subnets commons.Subnets, ok bool)
}

type topicIndex interface {
	PeersByTopic() map[string][]peer.ID
}

// operatorDataStore exposes this node's own operator identity. The public key is
// populated from config at startup; the id and owner address are filled in once the
// operator's registration event has been synced from the chain, which OperatorIDReady
// reports. A node started without an operator key holds an empty OperatorData.
type operatorDataStore interface {
	GetOperatorData() *registrystorage.OperatorData
	OperatorIDReady() bool
}
