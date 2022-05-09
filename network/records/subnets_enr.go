package records

import (
	"encoding/hex"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"io"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, count int, added []int64, removed []int64) error {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil {
		return errors.Wrap(err, "could not read subnets entry from enr")
	}
	if len(subnets) == 0 { // not exist, creating slice
		subnets = make([]byte, count)
	}
	for _, i := range added {
		subnets[i] = 1
	}
	for _, i := range removed {
		subnets[i] = 0
	}
	return SetSubnetsEntry(node, subnets)
}

// SetSubnetsEntry adds subnets entry to our enode.LocalNode
func SetSubnetsEntry(node *enode.LocalNode, subnets []byte) error {
	node.Set(SubnetsEntry(hex.EncodeToString(subnets)))
	return nil
}

// GetSubnetsEntry extracts the value of subnets entry from some record
func GetSubnetsEntry(record *enr.Record) ([]byte, error) {
	se := new(SubnetsEntry)
	if err := record.Load(se); err != nil {
		if enr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return hex.DecodeString(string(*se))
}

// SubnetsEntry holds the subnets that the operator is subscribed to
type SubnetsEntry string

// ENRKey implements enr.Entry, returns the entry key
func (se SubnetsEntry) ENRKey() string { return "subnets" }

// EncodeRLP implements rlp.Encoder
func (se SubnetsEntry) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []byte(se))
}

// DecodeRLP implements rlp.Decoder
func (se *SubnetsEntry) DecodeRLP(s *rlp.Stream) error {
	buf, err := s.Bytes()
	if err != nil {
		return err
	}
	*se = SubnetsEntry(buf)
	return nil
}
