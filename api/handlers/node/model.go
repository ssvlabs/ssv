package node

import (
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
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

type healthStatus struct{ err error }

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
