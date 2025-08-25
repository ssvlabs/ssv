package connections

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/network/peers"
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

// BadPeerFilter avoids connecting to a bad peer
func BadPeerFilter(n peers.Index) HandshakeFilter {
	return func(senderID peer.ID, sni *records.NodeInfo) error {
		if n.IsBad(senderID) {
			return errors.New("bad peer")
		}
		return nil
	}
}

// TODO: filter based on domaintype
