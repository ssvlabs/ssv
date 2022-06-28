package records

import (
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, count int, added []int, removed []int) error {
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
	subnetsVec := bitfield.NewBitvector128()
	for i, subnet := range subnets {
		subnetsVec.SetBitAt(uint64(i), subnet > 0)
	}
	fmt.Println("subnetsVec:", subnetsVec)
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
