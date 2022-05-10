package records

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
)

// TODO: remove this file in the future as we won't need node type and operator id entries post fork v1

// NodeType indicate node operation type. In purpose for distinguish between different types of peers
type NodeType int32

func (nt NodeType) String() string {
	switch nt {
	case Operator:
		return "operator"
	case Exporter:
		return "exporter"
	}
	return "unknown"
}

// NodeTypes are const types for NodeType
const (
	Unknown NodeType = iota
	Operator
	Exporter
)

// NodeTypeEntry holds the node type
type NodeTypeEntry uint16

// ENRKey implements enr.Entry, returns the entry key
func (nte NodeTypeEntry) ENRKey() string { return "type" }

// SetNodeTypeEntry adds operator-public-key-hash entry ('oid') to the node
func SetNodeTypeEntry(node *enode.LocalNode, nodeType NodeType) error {
	node.Set(NodeTypeEntry(nodeType))
	return nil
}

// GetNodeTypeEntry extracts the value of operator-public-key-hash entry ('oid')
func GetNodeTypeEntry(record *enr.Record) (NodeType, error) {
	var nte NodeTypeEntry
	if err := record.Load(&nte); err != nil {
		if enr.IsNotFound(err) {
			return Unknown, nil
		}
		return Unknown, err
	}
	return NodeType(nte), nil
}

// OperatorIDEntry holds the operator id
type OperatorIDEntry string

// ENRKey implements enr.Entry, returns the entry key
func (oid OperatorIDEntry) ENRKey() string { return "oid" }

// EncodeRLP implements rlp.Encoder, required because operator id is a string
func (oid OperatorIDEntry) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []byte(oid))
}

// DecodeRLP implements rlp.Decoder, required because operator id is a string
func (oid *OperatorIDEntry) DecodeRLP(s *rlp.Stream) error {
	buf, err := s.Bytes()
	if err != nil {
		return err
	}
	*oid = OperatorIDEntry(buf)
	return nil
}

// SetOperatorIDEntry adds operator-id entry ('oid') to the node
func SetOperatorIDEntry(node *enode.LocalNode, operatorID string) error {
	node.Set(OperatorIDEntry(operatorID))
	return nil
}

// GetOperatorIDEntry extracts the value of operator-id entry ('oid')
func GetOperatorIDEntry(record *enr.Record) (string, error) {
	oid := new(OperatorIDEntry)
	if err := record.Load(oid); err != nil {
		if enr.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(*oid), nil
}
