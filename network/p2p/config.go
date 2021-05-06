package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)
// Config - describe the config options for p2p network
type Config struct {
	DiscoveryType       string
	BootstrapNodeAddr   []string
	Discv5BootStrapAddr []string
	UDPPort             int
	TCPPort             int
	HostAddress         string
	HostDNS             string
	HostID              peer.ID
	//TopicName           string
	Topics map[string]*pubsub.Topic
	Subs   []*pubsub.Subscription
}
