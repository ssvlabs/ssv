package connections

import (
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/network/records"
)

// NetworkIDFilter determines whether we will connect to the given node by any matching network ID.
func NetworkIDFilter(networkIDs ...string) HandshakeFilter {
	allowed := make(map[string]struct{}, len(networkIDs))
	for _, id := range networkIDs {
		allowed[id] = struct{}{}
	}

	return func(sender peer.ID, ni *records.NodeInfo) error {
		nid := ni.GetNodeInfo().NetworkID
		if _, ok := allowed[nid]; !ok {
			return fmt.Errorf("mismatching domain type (want one of %v, got %s)", networkIDs, nid)
		}
		return nil
	}
}

// BadPeerFilter avoids connecting to a peer isBadPeer reports as bad
func BadPeerFilter(isBadPeer IsBadPeerF) HandshakeFilter {
	return func(senderID peer.ID, sni *records.NodeInfo) error {
		if isBadPeer(senderID) {
			return errors.New("bad peer")
		}
		return nil
	}
}

// TODO: filter based on domaintype
