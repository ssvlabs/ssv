package discovery

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"io"
	"strconv"
)

const (
	// SubnetsCount is the count of subnets in the network
	SubnetsCount = 128
)

var regPool = format.NewRegexpPool("\\w+:bloxstaking\\.ssv\\.(\\d+)")

// nsToSubnet converts the given topic to subnet
// TODO: return other value than zero upon failure?
func nsToSubnet(ns string) int64 {
	r, done := regPool.Get()
	defer done()
	found := r.FindStringSubmatch(ns)
	if len(found) != 2 {
		return -1
	}
	val, err := strconv.ParseUint(found[1], 10, 64)
	if err != nil {
		return -1
	}
	return int64(val)
}

// isSubnet checks if the given string is a subnet string
func isSubnet(ns string) bool {
	r, done := regPool.Get()
	defer done()
	return r.MatchString(ns)
}

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
