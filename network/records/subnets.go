package records

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"strconv"
	"strings"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, count int, added []int, removed []int) ([]byte, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil {
		return nil, errors.Wrap(err, "could not read subnets entry")
	}
	orig := make([]byte, len(subnets))
	if len(subnets) == 0 { // not exist, creating slice
		subnets = make([]byte, count)
	} else {
		copy(orig, subnets)
	}
	for _, i := range added {
		subnets[i] = 1
	}
	for _, i := range removed {
		subnets[i] = 0
	}
	if bytes.Equal(orig, subnets) {
		return nil, nil
	}
	if err := SetSubnetsEntry(node, subnets); err != nil {
		return nil, errors.Wrap(err, "could not update subnets entry")
	}
	return subnets, nil
}

// SetSubnetsEntry adds subnets entry to our enode.LocalNode
func SetSubnetsEntry(node *enode.LocalNode, subnets []byte) error {
	subnetsVec := bitfield.NewBitvector128()
	for i, subnet := range subnets {
		subnetsVec.SetBitAt(uint64(i), subnet > 0)
	}
	node.Set(enr.WithEntry("subnets", &subnetsVec))
	return nil
}

// GetSubnetsEntry extracts the value of subnets entry from some record
func GetSubnetsEntry(record *enr.Record) ([]byte, error) {
	subnetsVec := bitfield.NewBitvector128()
	if err := record.Load(enr.WithEntry("subnets", &subnetsVec)); err != nil {
		if enr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var res []byte
	for i := uint64(0); i < subnetsVec.Len(); i++ {
		val := byte(0)
		if subnetsVec.BitAt(i) {
			val = 1
		}
		res = append(res, val)
	}
	return res, nil
}

// Subnets holds all the subscribed subnets of a specific node
type Subnets []byte

func (s Subnets) String() string {
	subnetsVec := bitfield.NewBitvector128()
	for i, subnet := range s {
		subnetsVec.SetBitAt(uint64(i), subnet > 0)
	}
	return hex.EncodeToString(subnetsVec.Bytes())
}

// FromString parses a given subnet string
func (s Subnets) FromString(subnetsStr string) (Subnets, error) {
	subnetsStr = strings.Replace(subnetsStr, "0x", "", 1)
	for i := 0; i < len(subnetsStr); i++ {
		val, err := strconv.ParseUint(string(subnetsStr[i]), 16, 8)
		if err != nil {
			return nil, err
		}
		mask := fmt.Sprintf("%04b", val)
		for j := 0; j < len(mask); j++ {
			val, err := strconv.ParseUint(string(mask[j]), 2, 8)
			if err != nil {
				return nil, err
			}
			s = append(s, uint8(val))
		}
	}
	return s, nil
}
