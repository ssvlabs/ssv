package node

import (
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	healthyPeerCount = 20
	healthyInbounds  = 4
)

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
	SubnetsHex    string           `json:"subnets"`
	Version       string           `json:"version"`
}

type identityJSON struct {
	PeerID    peer.ID  `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Subnets   string   `json:"subnets"`
	Version   string   `json:"version"`
	NetworkID string   `json:"network_id"`

	// This node's operator identity, as opposed to the network identity above. These are
	// omitted rather than zeroed because their absence is meaningful. A node running
	// without an operator key (exporter mode) has none of them. OperatorID and
	// OwnerAddress additionally stay absent until this operator's registration event has
	// been synced, so a public key with no id means "registered but not yet observed, or
	// not registered at all" - the public key is known from config at startup either way.
	OperatorID        spectypes.OperatorID `json:"operator_id,omitempty"`
	OperatorPublicKey string               `json:"operator_public_key,omitempty"`
	OwnerAddress      string               `json:"owner_address,omitempty"`
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
