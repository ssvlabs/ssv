package p2p

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"time"
)

// Config - describe the config options for p2p network
type Config struct {
	// yaml/env arguments
	Enr              string        `yaml:"Enr" env:"ENR_KEY" env-description:"enr used in discovery" env-default:""`
	DiscoveryType    string        `yaml:"DiscoveryType" env:"DISCOVERY_TYPE_KEY" env-description:"Method to use in discovery" env-default:"discv5"`
	TCPPort          int           `yaml:"TcpPort" env:"TCP_PORT" env-default:"13000"`
	UDPPort          int           `yaml:"UdpPort" env:"UDP_PORT" env-default:"12000"`
	HostAddress      string        `yaml:"HostAddress" env:"HOST_ADDRESS" env-required:"true" env-description:"External ip node is exposed for discovery"`
	HostDNS          string        `yaml:"HostDNS" env:"HOST_DNS" env-description:"External DNS node is exposed for discovery"`
	RequestTimeout   time.Duration `yaml:"RequestTimeout" env:"P2P_REQUEST_TIMEOUT"  env-default:"5s"`
	MaxBatchResponse uint64        `yaml:"MaxBatchResponse" env:"P2P_MAX_BATCH_RESPONSE" env-default:"50" env-description:"maximum number of returned objects in a batch"`
	MaxPeers         int           `yaml:"MaxPeers" env:"P2P_MAX_PEERS" env-default:"250" env-description:"Connected peers limit, starting from this number only relevant nodes will be connectable"`
	PubSubTraceOut   string        `yaml:"PubSubTraceOut" env:"PUBSUB_TRACE_OUT" env-description:"File path to hold collected pubsub traces"`
	//PubSubTracer     string        `yaml:"PubSubTracer" env:"PUBSUB_TRACER" env-description:"A remote tracer that collects pubsub traces"`

	NetworkTrace          bool `yaml:"NetworkTrace" env:"NETWORK_TRACE" env-description:"A boolean flag to turn on network debugging"`
	NetworkDiscoveryTrace bool `yaml:"NetworkDiscoveryTrace" env:"NETWORK_DISCOVERY_TRACE" env-description:"A boolean flag to turn on network discovery debugging (discv5)"`

	UseMainTopic bool `yaml:"UseMainTopic" env:"USE_MAIN_TOPIC" env-description:"A boolean flag to turn on usage of main topic"`

	ExporterPeerID string `yaml:"ExporterPeerID" env:"EXPORTER_PEER_ID"  env-default:"16Uiu2HAkvaBh2xjstjs1koEx3jpBn5Hsnz7Bv8pE4SuwFySkiAuf"  env-description:"peer id of exporter"`

	Fork forks.Fork

	// objects / instances
	HostID        peer.ID
	Topics        map[string]*pubsub.Topic
	BootnodesENRs []string

	// NetworkPrivateKey is used for network identity
	NetworkPrivateKey *ecdsa.PrivateKey
	// OperatorPrivateKey is used for operator identity
	OperatorPrivateKey *rsa.PrivateKey
	// ReportLastMsg whether to report last msg metric
	ReportLastMsg bool
	// NodeType differentiate exporters peers from others
	NodeType NodeType
}

// NodeType indicate node operation type. In purpose for distinguish between different types of peers
type NodeType int64

// NodeTypes are const types for NodeType
const (
	Unknown NodeType = iota
	Operator
	Exporter
)

// FromString convert string to NodeType. If not exist, return Unknown
func (nt NodeType) FromString(nodeType string) NodeType {
	switch nodeType {
	case "operator":
		return Operator
	case "exporter":
		return Exporter
	case "unknown":
		return Unknown
	}
	return nt
}

func (nt NodeType) String() string {
	switch nt {
	case Operator:
		return "operator"
	case Exporter:
		return "exporter"
	}
	return "unknown"
}

// TransformEnr converts defaults enr value and convert it to slice
func TransformEnr(enr string) []string {
	if len(enr) == 0 {
		// stage enr
		//enr = "enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"

		// prod enr
		//internal ip
		//enr = "enr:-LK4QPbCB0Mw_8ji7D02OwXmqSRZe9wTmitle_cQnECIl-5GBPH9PH__eUpdeiI_t122inm62uTgO9CptbGNLKNId7gBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArsBGGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"
		//external ip
		enr = "enr:-LK4QMmL9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g"
	}
	return []string{
		enr,
	}
}

//
//// setupNetworkKey creates a private key if non configured
//func (n *p2pNetwork) setupNetworkKey(priv *ecdsa.PrivateKey) error {
//	if priv != nil {
//		n.privKey = priv
//	} else {
//		privKey, err := n.privateKey()
//		if err != nil {
//			return errors.Wrap(err, "Failed to generate p2p private key")
//		}
//		n.privKey = privKey
//	}
//	return nil
//}
