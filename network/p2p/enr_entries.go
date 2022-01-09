package p2p

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
)

// NodeTypeEntry holds the node type
type NodeTypeEntry uint16

// ENRKey implements enr.Entry, returns the entry key
func (nte NodeTypeEntry) ENRKey() string { return "type" }

// addOperatorIDEntry adds operator-public-key-hash entry ('oid') to the node
func addNodeTypeEntry(node *enode.LocalNode, nodeType NodeType) (*enode.LocalNode, error) {
	node.Set(NodeTypeEntry(nodeType))
	return node, nil
}

// extractOperatorIDEntry extracts the value of operator-public-key-hash entry ('oid')
func extractNodeTypeEntry(record *enr.Record) (NodeType, error) {
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

// addOperatorIDEntry adds operator-id entry ('oid') to the node
func addOperatorIDEntry(node *enode.LocalNode, pkHash string) (*enode.LocalNode, error) {
	node.Set(OperatorIDEntry(pkHash))
	return node, nil
}

// extractOperatorIDEntry extracts the value of operator-id entry ('oid')
func extractOperatorIDEntry(record *enr.Record) (*OperatorIDEntry, error) {
	oid := new(OperatorIDEntry)
	if err := record.Load(oid); err != nil {
		if enr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return oid, nil
}
