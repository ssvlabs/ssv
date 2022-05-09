package records

import (
	"encoding/json"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/record"
	"github.com/pkg/errors"
)

const (
	domain = "ssv-network"
)

var codec = []byte("/ssv/record")

func init() {
	// registers the NodeRecord as a valid type
	record.RegisterType(&NodeRecord{})
}

// NodeRecord implements record.Record
// it holds information of a specific node in the network.
type NodeRecord struct {
	// ForkV is the current fork version used by the node
	ForkV string
	// ENR the node record used by discv5
	ENR string
	//// Metadata contains node's general information
	//Metadata *NodeMetadata
}

// NewNodeRecord creates a new NodeRecord with the given fields
func NewNodeRecord(forkv, enr string) *NodeRecord {
	return &NodeRecord{
		forkv, enr,
	}
}

// Seal seals and encodes the record to be sent to other peers
func (nr *NodeRecord) Seal(privateKey crypto.PrivKey) ([]byte, error) {
	ev, err := record.Seal(nr, privateKey)
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
func (nr *NodeRecord) Consume(data []byte) error {
	evParsed, err := record.ConsumeTypedEnvelope(data, &NodeRecord{})
	if err != nil {
		return errors.Wrap(err, "could not consume envelope")
	}
	parsed, err := evParsed.Record()
	if err != nil {
		return errors.Wrap(err, "could not get record")
	}
	rec, ok := parsed.(*NodeRecord)
	if !ok {
		return errors.New("could not convert to NodeRecord")
	}
	*nr = *rec
	return nil
}

// Domain is the "signature domain" used when signing and verifying an record.Record
func (nr *NodeRecord) Domain() string {
	return domain
}

// Codec is a binary identifier for this type of record.record
func (nr *NodeRecord) Codec() []byte {
	return codec
}

// MarshalRecord converts a Record instance to a []byte, so that it can be used as an Envelope payload
func (nr *NodeRecord) MarshalRecord() ([]byte, error) {
	//metadata, err := nr.Metadata.Encode()
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not encode metadata")
	//}
	ser := newSerializable(
		nr.ForkV,
		nr.ENR,
		//hex.EncodeToString(metadata),
	)

	return json.Marshal(ser)
}

// UnmarshalRecord unmarshals a []byte payload into an instance of a particular Record type
func (nr *NodeRecord) UnmarshalRecord(data []byte) error {
	var ser serializable

	if err := json.Unmarshal(data, &ser); err != nil {
		return err
	}

	if len(ser.Entries) < 2 {
		return errors.New("not enough entries in node record")
	}
	nr.ForkV = ser.Entries[0]
	nr.ENR = ser.Entries[1]
	//rawMetadata, err := hex.DecodeString(ser.Entries[3])
	//if err != nil {
	//	return errors.Wrap(err, "could not decode metadata hex")
	//}
	//nr.Metadata = new(NodeMetadata)
	//if err := nr.Metadata.Decode(rawMetadata); err != nil {
	//	return errors.Wrap(err, "could not decode metadata")
	//}

	return nil
}

// Subnets returns the registered subnets, taken from the ENR
func (nr *NodeRecord) Subnets() ([]byte, error) {
	n, err := enode.Parse(enode.ValidSchemes, nr.ENR)
	if err != nil {
		return nil, errors.Wrap(err, "could not parse ENR")
	}
	return GetSubnetsEntry(n.Record())
}

// Seq returns the sequence number of the underlying ENR
func (nr *NodeRecord) Seq() (uint64, error) {
	n, err := enode.Parse(enode.ValidSchemes, nr.ENR)
	if err != nil {
		return 0, errors.Wrap(err, "could not parse ENR")
	}
	return n.Seq(), nil
}

// serializable is a struct that can be encoded w/o worries of different encoding implementations,
// e.g. JSON where an unordered map can be different across environments.
// it uses a slice of entries to keep ordered values
type serializable struct {
	Entries []string
}

func newSerializable(entries ...string) *serializable {
	return &serializable{
		Entries: entries,
	}
}
