package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/bloxapp/ssv/api"
	networkpeers "github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/nodeprobe"
)

const healthyPeersAmount = 10

type TopicIndex interface {
	PeersByTopic() ([]peer.ID, map[string][]peer.ID)
}

type healthStatus int

const (
	bad healthStatus = iota
	good
)

func (c healthStatus) String() string {
	str := [...]string{"bad", "good"}
	if c < 0 || int(c) >= len(str) {
		return "(unrecognized)"
	}
	return str[c]
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
	PeersHealthStatus               string `json:"peers_status"`
	BeaconConnectionHealthStatus    string `json:"beacon_health_status"`
	ExecutionConnectionHealthStatus string `json:"execution_health_status"`
	EventSyncHealthStatus           string `json:"event_sync_health_status"`
	LocalPortsListening             string `json:"local_port_listening"`
}

type Node struct {
	PeersIndex networkpeers.Index
	TopicIndex TopicIndex
	Network    network.Network
	NodeProber *nodeprobe.Prober
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
	resp := h.peers(peers)
	return api.Render(w, r, resp)
}

func (h *Node) Topics(w http.ResponseWriter, r *http.Request) error {
	allpeers, peerbytpc := h.TopicIndex.PeersByTopic()
	alland := AllPeersAndTopicsJSON{}
	tpcs := []topicIndexJSON{}
	for topic, peers := range peerbytpc {
		tpcs = append(tpcs, topicIndexJSON{TopicName: topic, Peers: peers})
	}
	alland.AllPeers = allpeers
	alland.PeersByTopic = tpcs

	return api.Render(w, r, alland)
}

func (h *Node) Health(w http.ResponseWriter, r *http.Request) error {
	ctx := context.Background()
	resp := healthCheckJSON{
		BeaconConnectionHealthStatus:    good.String(),
		ExecutionConnectionHealthStatus: good.String(),
		EventSyncHealthStatus:           good.String(),
		PeersHealthStatus:               good.String(),
	}
	// Check ports being used.
	addrs := h.Network.ListenAddresses()
	for _, addr := range addrs {
		if addr.String() == "/p2p-circuit" || addr.Decapsulate(multiaddr.StringCast("/ip4/0.0.0.0")) == nil {
			continue
		}
		resp.LocalPortsListening = addr.String()
	}
	// Performing various health checks.
	resp.BeaconConnectionHealthStatus = performHealthCheck(h.NodeProber.CheckBeaconNodeHealth, ctx)
	resp.ExecutionConnectionHealthStatus = performHealthCheck(h.NodeProber.CheckExecutionNodeHealth, ctx)
	resp.EventSyncHealthStatus = performHealthCheck(h.NodeProber.CheckEventSyncerHealth, ctx)
	// Check peers connection.
	var activePeerCount int
	peers := h.Network.Peers()
	for _, p := range h.peers(peers) {
		if p.Connectedness == "Connected" {
			activePeerCount++
		}
	}
	switch {
	case activePeerCount > 0 && activePeerCount < healthyPeersAmount:
		resp.PeersHealthStatus = fmt.Sprintf("%s: %d peers are connected", bad, activePeerCount)
	case activePeerCount == 0:
		resp.PeersHealthStatus = fmt.Sprintf("%s: %s", bad, "error: no peers are connected")
	}
	// Handle plain text content.
	if contentType := api.NegotiateContentType(r); contentType == api.ContentTypePlainText {
		str := fmt.Sprintf("%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n",
			"peers_status", resp.PeersHealthStatus,
			"beacon_health_status", resp.BeaconConnectionHealthStatus,
			"execution_health_status", resp.ExecutionConnectionHealthStatus,
			"event_sync_health_status", resp.EventSyncHealthStatus,
			"local_port_listening", resp.LocalPortsListening,
		)
		return api.Render(w, r, str)
	}
	return api.Render(w, r, resp)
}

func (h *Node) peers(peers []peer.ID) []peerJSON {
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
	return resp
}

func performHealthCheck(healthCheckFunc func(context.Context) error, ctx context.Context) string {
	if err := healthCheckFunc(ctx); err != nil {
		return fmt.Sprintf("%s: %s", bad, err.Error())
	}
	return good.String()
}
