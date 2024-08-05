package connections

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/network/records"
)

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkID string) HandshakeFilter {
	return func(sender peer.ID, ni *records.NodeInfo) error {
		nid := ni.GetNodeInfo().NetworkID
		if networkID != nid {
			return errors.Errorf("mismatching domain type (want %s, got %s)", networkID, nid)
		}
		return nil
	}
}

// TODO: filter based on domaintype
