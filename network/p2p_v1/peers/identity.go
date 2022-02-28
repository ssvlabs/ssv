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

// Identity contains information of a specific peer
type Identity struct {
	// ID is the peer.ID of the node, based on network key
	ID string
	// OperatorID holds a hash of the operator public key, based on operator key
	OperatorID string
	// ForkV is the current fork version used by the node
	ForkV string
	// Metadata contains node's general information
	Metadata map[string]string

	addrInfo peer.AddrInfo
}

// NewIdentity creates new identity object
func NewIdentity(id, oid, forkv string, metadata map[string]string) *Identity {
	return &Identity{
		ID:         id,
		OperatorID: oid,
		ForkV:      forkv,
		Metadata:   metadata,
	}
}

// AddrInfo returns the underlying peer.AddrInfo
func (i *Identity) AddrInfo() peer.AddrInfo {
	return i.addrInfo
}

// ExecutionNode is the "name/version" of the eth1 node
func (i *Identity) ExecutionNode() string {
	e, ok := i.Metadata[execNodeKey]
	if !ok {
		return Unknown
	}
	return e
}

// ConsensusNode is the "name/version" of the beacon node
func (i *Identity) ConsensusNode() string {
	c, ok := i.Metadata[consensusNodeKey]
	if !ok {
		return Unknown
	}
	return c
}

// NodeVersion is the ssv-node version
func (i *Identity) NodeVersion() string {
	v, ok := i.Metadata[nodeVersionKey]
	if !ok {
		return Unknown
	}
	return v
}

// Encode encodes the identity
func (i *Identity) Encode() ([]byte, error) {
	return json.Marshal(i)
}

// DecodeIdentity decodes the given encoded identity
func DecodeIdentity(encoded []byte) (*Identity, error) {
	var res Identity
	if err := json.Unmarshal(encoded, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
