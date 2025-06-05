package records

import (
	"encoding/json"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/record"
	"github.com/pkg/errors"
)

const domain = "ssv"

var nodeInfoCodec = []byte("ssv/nodeinfo")

// NodeInfo holds node's information such as network information.
// it implements record.Record so we can safely sign, exchange and verify the data.
// for more information see record.Envelope
type NodeInfo struct {
	// NetworkID is the id of the node's network
	NetworkID string
	// Metadata holds node's general information
	Metadata *NodeMetadata
}

// NewNodeInfo creates a new node info
func NewNodeInfo(networkID string) *NodeInfo {
	return &NodeInfo{
		NetworkID: networkID,
	}
}

// Seal seals and encodes the record to be sent to other peers
func (ni *NodeInfo) Seal(privateKey crypto.PrivKey) ([]byte, error) {
	ev, err := record.Seal(ni, privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not seal record")
	}

	data, err := ev.Marshal()
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal envelope")
	}
	return data, nil
}

// Consume takes a raw envelope and extracts the parsed record
func (ni *NodeInfo) Consume(data []byte) error {
	evParsed, err := record.ConsumeTypedEnvelope(data, &NodeInfo{})
	if err != nil {
		return errors.Wrap(err, "could not consume envelope")
	}
	parsed, err := evParsed.Record()
	if err != nil {
		return errors.Wrap(err, "could not get record")
	}
	rec, ok := parsed.(*NodeInfo)
	if !ok {
		return errors.New("could not convert to NodeRecord")
	}
	*ni = *rec
	return nil
}

// GetNodeInfo returns a value representation of the info
func (ni *NodeInfo) GetNodeInfo() *NodeInfo {
	return ni
}

// Domain is the "signature domain" used when signing and verifying an record.Record
func (ni *NodeInfo) Domain() string {
	return domain
}

// Codec is a binary identifier for this type of record.record
func (ni *NodeInfo) Codec() []byte {
	return nodeInfoCodec
}

// MarshalRecord converts a Record instance to a []byte, so that it can be used as an Envelope payload
func (ni *NodeInfo) MarshalRecord() ([]byte, error) {
	parts := []string{
		"", // Deprecated: ForkVersion is no longer used. Left for backward compatibility.
		ni.NetworkID,
	}
	if ni.Metadata != nil {
		rawMeta, err := ni.Metadata.Encode()
		if err != nil {
			return nil, errors.Wrap(err, "could not encode metadata")
		}
		parts = append(parts, string(rawMeta))
	}
	ser := newSerializable(parts...)

	return json.Marshal(ser)
}

// UnmarshalRecord unmarshals a []byte payload into an instance of a particular Record type
func (ni *NodeInfo) UnmarshalRecord(data []byte) error {
	var ser serializable

	if err := json.Unmarshal(data, &ser); err != nil {
		return err
	}

	if len(ser.Entries) < 1 {
		return errors.New("not enough entries in node info, fork version is required")
	}
	// Deprecated: ForkVersion is no longer used. Left for backward compatibility.
	// ni.ForkVersion = forksprotocol.ForkVersion(ser.Entries[0])

	if len(ser.Entries) < 2 {
		return errors.New("not enough entries in node info, network ID is required")
	}
	ni.NetworkID = ser.Entries[1]

	if len(ser.Entries) < 3 {
		return nil
	}
	ni.Metadata = new(NodeMetadata)
	err := ni.Metadata.Decode([]byte(ser.Entries[2]))
	if err != nil {
		return errors.Wrap(err, "could not decode metadata")
	}

	return nil
}

func (ni *NodeInfo) Clone() *NodeInfo {
	return &NodeInfo{
		NetworkID: ni.NetworkID,
		Metadata:  ni.Metadata.Clone(),
	}
}
