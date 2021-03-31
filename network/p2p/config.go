package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type Config struct {
	Local               bool
	BootstrapNodeAddr   []string
	Discv5BootStrapAddr []string
	UdpPort             int
	TcpPort             int
	HostAddress         string
	HostID              peer.ID
	TopicName           string
	Topic               *pubsub.Topic
	Sub                 *pubsub.Subscription
}
