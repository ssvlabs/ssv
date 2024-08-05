package connections

import (
	"encoding/hex"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"

	"github.com/ssvlabs/ssv/network/records"
)

// NetworkIDFilter determines whether we will connect to the given node by the network ID
func NetworkIDFilter(networkConfig networkconfig.NetworkConfig, network *p2pv1.Config) HandshakeFilter {
	return func(sender peer.ID, ni *records.NodeInfo) error {
		d := networkConfig.DomainType()
		domain := "0x" + hex.EncodeToString(d[:])
		bla := network.Network.DomainType()
		println("<<<<<<<<<<<<<<<<<<<<<<here>>>>>>>>>>>>>>>>>>>>>>>>>>")
		println(domain)
		println("0x" + hex.EncodeToString(bla[:]))
		println("<<<<<<<<<<<<<<<<<<<<<<here>>>>>>>>>>>>>>>>>>>>>>>>>>")
		nid := ni.GetNodeInfo().NetworkID
		if domain != nid {
			return errors.Errorf("mismatching domain type (want %s, got %s)", domain, nid)
		}
		return nil
	}
}

// TODO: filter based on domaintype
