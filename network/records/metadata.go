package records

import (
	"encoding/json"
)

// NodeMetadata holds node's general information
type NodeMetadata struct {
	// NodeVersion is the ssv-node version, it is a required field
	NodeVersion string
	// ExecutionNode is the "name/version" of the eth1 node
	ExecutionNode string
	// ConsensusNode is the "name/version" of the beacon node
	ConsensusNode string
	// Subnets represents the subnets that our node is subscribed to
	Subnets string
}

// Encode encodes the metadata into bytes
// TODO: switch to SSZ
func (nm *NodeMetadata) Encode() ([]byte, error) {
	// ser := newSerializable(
	//	nm.NodeVersion,
	//	nm.ConsensusNode,
	//	nm.ExecutionNode,
	//)

	return json.Marshal(nm)
}

// Decode decodes a raw payload into metadata
// TODO: switch to SSZ
func (nm *NodeMetadata) Decode(data []byte) error {
	// var ser serializable

	if err := json.Unmarshal(data, nm); err != nil {
		return err
	}

	return nil
}
