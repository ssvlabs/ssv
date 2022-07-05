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
