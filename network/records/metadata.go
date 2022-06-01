package records

import (
	"encoding/json"
)

// NodeMetadata holds node's general information
type NodeMetadata struct {
	// NodeVersion is the ssv-node version, it is a required field
	NodeVersion string
	// OperatorID holds a hash of the operator public key, based on operator key
	OperatorID string
	// ExecutionNode is the "name/version" of the eth1 node
	ExecutionNode string
	// ConsensusNode is the "name/version" of the beacon node
	ConsensusNode string
}

// Encode encodes the metadata into bytes
// TODO: switch to SSZ
func (nm *NodeMetadata) Encode() ([]byte, error) {
	//ser := newSerializable(
	//	nm.NodeVersion,
	//	nm.OperatorID,
	//	nm.ConsensusNode,
	//	nm.ExecutionNode,
	//)

	return json.Marshal(nm)
}

// Decode decodes a raw payload into metadata
// TODO: switch to SSZ
func (nm *NodeMetadata) Decode(data []byte) error {
	//var ser serializable

	if err := json.Unmarshal(data, nm); err != nil {
		return err
	}

	//nm.NodeVersion = ""
	//nm.ConsensusNode = ""
	//nm.ExecutionNode = ""
	//nm.OperatorID = ""
	//
	//if len(ser.Entries) < 1 {
	//	return errors.New("not enough entries in node metadata, node version is required")
	//}
	//nm.NodeVersion = ser.Entries[0]
	//if len(ser.Entries) < 2 {
	//	return nil
	//}
	//nm.ConsensusNode = ser.Entries[1]
	//if len(ser.Entries) < 3 {
	//	return nil
	//}
	//nm.ExecutionNode = ser.Entries[2]
	//if len(ser.Entries) < 4 {
	//	return nil
	//}
	//nm.OperatorID = ser.Entries[3]

	return nil
}
