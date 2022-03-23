package peers

import (
	"encoding/json"
	"github.com/libp2p/go-libp2p-core/peer"
)

const (
	execNodeKey      = "execution"
	consensusNodeKey = "consensus"
	nodeVersionKey   = "version"
	// Unknown is returned for unknown values
	Unknown = "unknown"
)

// NodeInfo contains information of a specific peer
type NodeInfo struct {
	// ID is the peer.ID of the node, based on network key
	ID string
	// OperatorID holds a hash of the operator public key, based on the operator key.
	OperatorID string
	// ForkV is the current fork version used by the node
	ForkV string
	// Subnets holds the subnets that the node is interested in
	Subnets []bool
	// Metadata contains node's general information
	Metadata map[string]string

	addrInfo peer.AddrInfo
}

// NewNodeInfo creates new identity object
func NewNodeInfo(id, oid, forkv string, subnets []bool, metadata map[string]string) *NodeInfo {
	return &NodeInfo{
		ID:         id,
		OperatorID: oid,
		ForkV:      forkv,
		Subnets:    subnets,
		Metadata:   metadata,
	}
}

// AddrInfo returns the underlying peer.AddrInfo
func (ni *NodeInfo) AddrInfo() peer.AddrInfo {
	return ni.addrInfo
}

// ExecutionNode is the "name/version" of the eth1 node
func (ni *NodeInfo) ExecutionNode() string {
	e, ok := ni.Metadata[execNodeKey]
	if !ok {
		return Unknown
	}
	return e
}

// ConsensusNode is the "name/version" of the beacon node
func (ni *NodeInfo) ConsensusNode() string {
	c, ok := ni.Metadata[consensusNodeKey]
	if !ok {
		return Unknown
	}
	return c
}

// NodeVersion is the ssv-node version
func (ni *NodeInfo) NodeVersion() string {
	v, ok := ni.Metadata[nodeVersionKey]
	if !ok {
		return Unknown
	}
	return v
}

// NodeType returns the node type
func (ni *NodeInfo) NodeType() string {
	if len(ni.OperatorID) == 0 {
		return "exporter"
	}
	return "operator"
}

// Encode encodes the identity
func (ni *NodeInfo) Encode() ([]byte, error) {
	return json.Marshal(ni)
}

// DecodeNodeInfo decodes the given encoded identity
func DecodeNodeInfo(encoded []byte) (*NodeInfo, error) {
	var res NodeInfo
	if err := json.Unmarshal(encoded, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
