package discovery

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
)

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

// setOperatorIDEntry adds operator-id entry ('oid') to the node
func setOperatorIDEntry(node *enode.LocalNode, operatorID string) (*enode.LocalNode, error) {
	node.Set(OperatorIDEntry(operatorID))
	return node, nil
}

// getOperatorIDEntry extracts the value of operator-id entry ('oid')
func getOperatorIDEntry(record *enr.Record) (string, error) {
	oid := new(OperatorIDEntry)
	if err := record.Load(oid); err != nil {
		if enr.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return string(*oid), nil
}
