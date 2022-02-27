package peers

import "github.com/libp2p/go-libp2p-core/peer"

const (
	ExecutionNodeKey = "execution"
	ConsensusNodeKey = "consensus"
	NodeVersionKey   = "version"

	Unknown = "unknown"
)

// Identity contains information of a specific peer
type Identity struct {
	// ID is the peer.ID of the node, based on network key
	ID string
	// OperatorID holds a hash of the operator public key, based on operator key
	OperatorID string
	// ForkV is the current fork used by the node
	ForkV string
	// Metadata contains node's general information
	Metadata map[string]string

	addrInfo peer.AddrInfo
}

// AddrInfo returns the underlying peer.AddrInfo
func (i *Identity) AddrInfo() peer.AddrInfo {
	return i.addrInfo
}

// ExecutionNode is the "name/version" of the eth1 node
func (i *Identity) ExecutionNode() string {
	e, ok := i.Metadata[ExecutionNodeKey]
	if !ok {
		return Unknown
	}
	return e
}

// ConsensusNode is the "name/version" of the beacon node
func (i *Identity) ConsensusNode() string {
	c, ok := i.Metadata[ConsensusNodeKey]
	if !ok {
		return Unknown
	}
	return c
}

// NodeVersion is the ssv-node version
func (i *Identity) NodeVersion() string {
	v, ok := i.Metadata[NodeVersionKey]
	if !ok {
		return Unknown
	}
	return v
}
