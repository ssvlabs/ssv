package handlers

import (
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/bloxapp/ssv/api"
	networkpeers "github.com/bloxapp/ssv/network/peers"
)

type TopicIndex interface {
	PeersByTopic() ([]peer.ID, map[string][]peer.ID)
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

type healthCheckJSON struct {
	PeersConnectionHealth  string `json:"peers_connection_health"`
	BeaconConnectionHealth string `json:"beacon_connection_health"`
}

type Node struct {
	PeersIndex networkpeers.Index
	TopicIndex TopicIndex
	Network    network.Network
}

func (h *Node) Identity(w http.ResponseWriter, r *http.Request) error {
	nodeInfo := h.PeersIndex.Self()
	resp := identityJSON{
		PeerID:  h.Network.LocalPeer(),
		Subnets: nodeInfo.Metadata.Subnets,
		Version: nodeInfo.Metadata.NodeVersion,
	}
	for _, addr := range h.Network.ListenAddresses() {
		resp.Addresses = append(resp.Addresses, addr.String())
	}
	return api.Render(w, r, resp)
}

func (h *Node) Peers(w http.ResponseWriter, r *http.Request) error {
	peers := h.Network.Peers()
	resp := make([]peerJSON, len(peers))
	for i, id := range peers {
		resp[i] = peerJSON{
			ID:            id,
			Connectedness: h.Network.Connectedness(id).String(),
			Subnets:       h.PeersIndex.GetPeerSubnets(id).String(),
		}

		for _, addr := range h.Network.Peerstore().Addrs(id) {
			resp[i].Addresses = append(resp[i].Addresses, addr.String())
		}

		conns := h.Network.ConnsToPeer(id)
		for _, conn := range conns {
			resp[i].Connections = append(resp[i].Connections, connectionJSON{
				Address:   conn.RemoteMultiaddr().String(),
				Direction: conn.Stat().Direction.String(),
			})
		}

		nodeInfo := h.PeersIndex.NodeInfo(id)
		if nodeInfo == nil {
			continue
		}
		resp[i].Version = nodeInfo.Metadata.NodeVersion
	}
	return api.Render(w, r, resp)
}

func (h *Node) Topics(w http.ResponseWriter, r *http.Request) error {
	allpeers, peerbytpc := h.TopicIndex.PeersByTopic()
	alland := AllPeersAndTopicsJSON{}
	tpcs := []topicIndexJSON{}
	for topic, peerz := range peerbytpc {
		tpcs = append(tpcs, topicIndexJSON{TopicName: topic, Peers: peerz})
	}
	alland.AllPeers = allpeers
	alland.PeersByTopic = tpcs

	return api.Render(w, r, alland)
}

func (h *Node) Health(w http.ResponseWriter, r *http.Request) error {
	resp := healthCheckJSON{}
	switch l := len(h.Network.Peers()); {
	case l == 0:
		resp.PeersConnectionHealth = "red"
	case l > 0 && l <= 10:
		resp.PeersConnectionHealth = "yellow"
	case l > 10:
		resp.PeersConnectionHealth = "green"
	}
	return api.Render(w, r, resp)
}
