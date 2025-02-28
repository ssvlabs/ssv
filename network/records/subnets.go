package records

import (
	"bytes"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, count int, added []uint64, removed []uint64) ([]byte, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
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
