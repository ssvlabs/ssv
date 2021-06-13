package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"time"
)

// Config - describe the config options for p2p network
type Config struct {
	Discv5BootStrapAddr []string
	BootstrapNodeAddr   []string
	// TODO: resolve redundancy with Enr and BootstrapNodeAddr
	Enr           string `yaml:"Enr" env:"ENR_KEY" env-description:"enr used in discovery" env-default:""`
	DiscoveryType string `yaml:"DiscoveryType" env-default:"mdns"`
	TCPPort       int    `yaml:"TcpPort" env-default:"13000"`
	UDPPort       int    `yaml:"UdpPort" env-default:"12000"`
	HostAddress   string `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS       string `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`

	HostID peer.ID
	Topics map[string]*pubsub.Topic
	Subs   []*pubsub.Subscription

	// params
	MaxBatchResponse uint64 // maximum number of returned objects in a batch
	RequestTimeout   time.Duration
}
