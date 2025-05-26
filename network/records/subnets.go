package records

import (
	"fmt"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/network/commons"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, added []uint64, removed []uint64) (commons.Subnets, bool, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return commons.Subnets{}, false, fmt.Errorf("could not read subnets entry: %w", err)
	}

	orig := subnets
	for _, i := range added {
		subnets.Set(i)
	}
	for _, i := range removed {
		subnets.Clear(i)
	}
	if orig == subnets {
		return commons.Subnets{}, false, nil
	}
	if err := SetSubnetsEntry(node, subnets); err != nil {
		return commons.Subnets{}, false, fmt.Errorf("could not update subnets entry: %w", err)
	}
	return subnets, true, nil
}
