package connections

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/network/records"
)

var AllowedDifference = 30 * time.Second

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkID string) HandshakeFilter {
	return func(sender peer.ID, ani records.AnyNodeInfo) error {
		nid := ani.GetNodeInfo().NetworkID
		if networkID != nid {
			return errors.Errorf("networkID '%s' instead of '%s'", nid, networkID)
		}
		return nil
	}
}
