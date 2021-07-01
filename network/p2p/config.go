package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"time"
)

// Config - describe the config options for p2p network
type Config struct {
	// yaml/env arguments
	Enr              string        `yaml:"Enr" env:"ENR_KEY" env-description:"enr used in discovery" env-default:""`
	DiscoveryType    string        `yaml:"DiscoveryType" env:"DISCOVERY_TYPE_KEY" env-description:"Method to use in discovery" env-default:"mdns"`
	TCPPort          int           `yaml:"TcpPort" env:"TCP_PORT" env-default:"13000"`
	UDPPort          int           `yaml:"UdpPort" env:"UDP_PORT" env-default:"12000"`
	HostAddress      string        `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS          string        `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`
	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"5s"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-description:"maximum number of returned objects in a batch"`

	// objects / instances
	HostID              peer.ID
	Topics              map[string]*pubsub.Topic
	Subs                []*pubsub.Subscription
	Discv5BootStrapAddr []string
}

// TransformEnr converts defaults enr value and convert it to slice
func TransformEnr(enr string) []string {
	if len(enr) == 0 {
		// internal ip
		//enr := "enr:-LK4QDAmZK-69qRU5q-cxW6BqLwIlWoYH-BoRlX2N7D9rXBlM7OJ9tWRRtryqvCW04geHC_ab8QmWT9QULnT0Tc5S1cBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"
		// external ip
		enr = "enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"
		// ssh
		//enr := "enr:-LK4QAkFwcROm9CByx3aabpd9Muqxwj8oQeqnr7vm8PAA8l1ZbDWVZTF_bosINKhN4QVRu5eLPtyGCccRPb3yKG2xjcBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArqAOOJc2VjcDI1NmsxoQMCphx1UQ1PkBsdOb-4FRiSWM4JE7HoDarAzOp82SO4s4N0Y3CCE4iDdWRwgg-g"
	}
	return []string{
		enr,
	}
}
