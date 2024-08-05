package connections

import (
	"encoding/hex"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv/networkconfig"

	"github.com/ssvlabs/ssv/network/records"
)

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(network networkconfig.NetworkConfig) HandshakeFilter {
	return func(sender peer.ID, ni *records.NodeInfo) error {
		d := network.DomainType()
		domain := "0x" + hex.EncodeToString(d[:])
		nid := ni.GetNodeInfo().NetworkID
		if domain != nid {
			return errors.Errorf("mismatching domain type (want %s, got %s)", domain, nid)
		}
		return nil
	}
}

// TODO: filter based on domaintype
