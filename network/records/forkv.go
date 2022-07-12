package records

import (
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
)

// ForkVersionEntry holds the fork version of the node
type ForkVersionEntry string

// ENRKey implements enr.Entry, returns the entry key
func (fv ForkVersionEntry) ENRKey() string { return "forkv" }

// EncodeRLP implements rlp.Encoder, required because fork version is a string
func (fv ForkVersionEntry) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []byte(fv))
}

// DecodeRLP implements rlp.Decoder, required because fork version is a string
func (fv *ForkVersionEntry) DecodeRLP(s *rlp.Stream) error {
	buf, err := s.Bytes()
	if err != nil {
		return err
	}
	*fv = ForkVersionEntry(buf)
	return nil
}

// SetForkVersionEntry adds operator-id entry ('oid') to the node
func SetForkVersionEntry(node *enode.LocalNode, forkv string) error {
	node.Set(ForkVersionEntry(forkv))
	return nil
}

// GetForkVersionEntry extracts the value of operator-id entry ('oid')
func GetForkVersionEntry(record *enr.Record) (forksprotocol.ForkVersion, error) {
	oid := new(ForkVersionEntry)
	if err := record.Load(oid); err != nil {
		if enr.IsNotFound(err) {
			// if not found, assuming v0 for compatibility
			return forksprotocol.GenesisForkVersion, nil
		}
		return "", err
	}
	return forksprotocol.ForkVersion(*oid), nil
}
