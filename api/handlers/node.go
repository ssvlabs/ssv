package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/api"
	networkpeers "github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/nodeprobe"
)

const (
	healthyPeerCount = 20
	healthyInbounds  = 4
)

type TopicIndex interface {
	PeersByTopic() map[string][]peer.ID
}

type AllPeersAndTopicsJSON struct {
	AllPeers     []peer.ID        `json:"all_peers"`
	PeersByTopic []topicIndexJSON `json:"peers_by_topic"`
}

type topicIndexJSON struct {
	TopicName string    `json:"topic"`
	Peers     []peer.ID `json:"peers"`
}

type connectionJSON struct {
	Address   string `json:"address"`
	Direction string `json:"direction"`
}

type peerJSON struct {
	ID            peer.ID          `json:"id"`
	Addresses     []string         `json:"addresses"`
	Connections   []connectionJSON `json:"connections"`
	Connectedness string           `json:"connectedness"`
	Subnets       string           `json:"subnets"`
	Version       string           `json:"version"`
}

type identityJSON struct {
	PeerID    peer.ID  `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Subnets   string   `json:"subnets"`
	Version   string   `json:"version"`
}

type healthStatus struct {
	err error
}

func (h healthStatus) MarshalJSON() ([]byte, error) {
	if h.err == nil {
		return json.Marshal("good")
	}
	return json.Marshal(fmt.Sprintf("bad: %s", h.err.Error()))
}

type healthCheckJSON struct {
	P2P           healthStatus `json:"p2p"`
	BeaconNode    healthStatus `json:"beacon_node"`
	ExecutionNode healthStatus `json:"execution_node"`
	EventSyncer   healthStatus `json:"event_syncer"`
	Advanced      struct {
		Peers           int      `json:"peers"`
		InboundConns    int      `json:"inbound_conns"`
		OutboundConns   int      `json:"outbound_conns"`
		ListenAddresses []string `json:"p2p_listen_addresses"`
	} `json:"advanced"`
}

func (hc healthCheckJSON) String() string {
	b, err := json.MarshalIndent(hc, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling healthCheckJSON: %s", err.Error())
	}
	return string(b)
}

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
