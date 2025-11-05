package records

import (
	"encoding/json"
	"fmt"

	"github.com/ssvlabs/ssv/network/commons"
)

const subnetsLength = int(commons.SubnetsCount) / 4 // each char in the string encodes status of 4 subnets

// NodeMetadata holds node's general information
type NodeMetadata struct {
	// NodeVersion is the ssv-node version, it is a required field
	NodeVersion string
	// ExecutionNode is the "name/version" of the eth1 node
	ExecutionNode string
	// ConsensusNode is the "name/version" of the beacon node
	ConsensusNode string
	// SubnetsHex represents the subnets that our node is subscribed to
	SubnetsHex string
}

// Encode encodes the metadata into bytes
func (nm *NodeMetadata) Encode() ([]byte, error) {
	if len(nm.SubnetsHex) != subnetsLength {
		return nil, fmt.Errorf("invalid subnets length %d", len(nm.SubnetsHex))
	}

	return json.Marshal(nm)
}

// Decode decodes a raw payload into metadata
func (nm *NodeMetadata) Decode(data []byte) error {
	if err := json.Unmarshal(data, nm); err != nil {
		return err
	}
	if len(nm.SubnetsHex) != subnetsLength {
		return fmt.Errorf("invalid subnets length %d", len(nm.SubnetsHex))
	}
	return nil
}

func (nm *NodeMetadata) Clone() *NodeMetadata {
	cpy := *nm
	return &cpy
}
