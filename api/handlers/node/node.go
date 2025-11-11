package node

import (
	"errors"
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/api"
	networkpeers "github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/nodeprobe"
)

type Node struct {
	listenAddresses []string
	peersIndex      networkpeers.Index
	topicIndex      TopicIndex
	network         network.Network

	nodeProber          *nodeprobe.Prober
	clNodeName          string
	elNodeName          string
	eventSyncerNodeName string
}

func NewNode(
	listenAddresses []string,
	peersIndex networkpeers.Index,
	network network.Network,
	topicIndex TopicIndex,
	nodeProber *nodeprobe.Prober,
	clNodeName string,
	elNodeName string,
	eventSyncerNodeName string,
) *Node {
	return &Node{
		listenAddresses:     listenAddresses,
		peersIndex:          peersIndex,
		topicIndex:          topicIndex,
		network:             network,
		nodeProber:          nodeProber,
		clNodeName:          clNodeName,
		elNodeName:          elNodeName,
		eventSyncerNodeName: eventSyncerNodeName,
	}
}

func (h *Node) Identity(w http.ResponseWriter, r *http.Request) error {
	nodeInfo := h.peersIndex.Self()
	resp := identityJSON{
		PeerID:  h.network.LocalPeer(),
		Subnets: nodeInfo.Metadata.Subnets,
		Version: nodeInfo.Metadata.NodeVersion,
	}
	for _, addr := range h.network.ListenAddresses() {
		resp.Addresses = append(resp.Addresses, addr.String())
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
	resp.BeaconNode = healthStatus{h.nodeProber.Probe(ctx, h.clNodeName)}
	resp.ExecutionNode = healthStatus{h.nodeProber.Probe(ctx, h.elNodeName)}
	resp.EventSyncer = healthStatus{h.nodeProber.Probe(ctx, h.eventSyncerNodeName)}

	return api.Render(w, r, resp)
}

func (h *Node) peers(peers []peer.ID) []peerJSON {
	resp := make([]peerJSON, len(peers))
	for i, id := range peers {
		subnets, _ := h.peersIndex.GetPeerSubnets(id)

		resp[i] = peerJSON{
			ID:            id,
			Connectedness: h.network.Connectedness(id).String(),
			Subnets:       subnets.String(),
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
		if nodeInfo == nil {
			continue
		}
		resp[i].Version = nodeInfo.Metadata.NodeVersion
	}
	return resp
}
