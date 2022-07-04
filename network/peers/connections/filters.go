package connections

import (
	"github.com/bloxapp/ssv/network/records"
	"github.com/pkg/errors"
)

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkID string) HandshakeFilter {
	return func(ni *records.NodeInfo) (bool, error) {
		if networkID != ni.NetworkID {
			return false, errors.Errorf("networkID '%s' instead of '%s'", ni.NetworkID, networkID)
		}
		return true, nil
	}
}

// SharedSubnetsFilter determines whether we will connect to the given node by the amount of shared subnets
func SharedSubnetsFilter(subnetsProvider func() records.Subnets, n int) HandshakeFilter {
	return func(ni *records.NodeInfo) (bool, error) {
		subnets := subnetsProvider()
		if len(subnets) == 0 {
			return true, nil
		}
		if len(ni.Metadata.Subnets) == 0 {
			return true, nil
		}
		nodeSubnets, err := records.Subnets{}.FromString(ni.Metadata.Subnets)
		if err != nil {
			return false, err
		}
		shared := records.SharedSubnets(subnets, nodeSubnets, n)
		if len(shared) < n {
			return false, nil
		}
		return true, nil
	}
}
