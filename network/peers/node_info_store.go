package peers

import (
	"fmt"
	"github.com/bloxapp/ssv/network/records"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// nodeInfoStore stores records.NodeInfo.
// libp2p's Peerstore (github.com/libp2p/go-libp2p-peerstore) is used for the persistence of node info.
type nodeInfoStore struct {
	network libp2pnetwork.Network
}

func newNodeInfoStore(network libp2pnetwork.Network) *nodeInfoStore {
	return &nodeInfoStore{
		network: network,
	}
}

// Add saves the given node info
func (pi *nodeInfoStore) Add(logger *zap.Logger, pid peer.ID, nodeInfo *records.NodeInfo) (bool, error) {
	raw, err := nodeInfo.MarshalRecord()
	if err != nil {
		return false, errors.Wrap(err, "could not marshal node info record")
	}
	if err := pi.network.Peerstore().Put(pid, formatInfoKey(nodeInfoKey), raw); err != nil {
		logger.Warn("could not save peer data", zap.Error(err), zap.String("peer", pid.String()))
		return false, err
	}
	return true, nil
}

// Get returns the corresponding node info
func (pi *nodeInfoStore) Get(pid peer.ID) (*records.NodeInfo, error) {
	// build identity object
	raw, err := pi.network.Peerstore().Get(pid, formatInfoKey(nodeInfoKey))
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var ni records.NodeInfo
	err = ni.UnmarshalRecord(raw.([]byte))
	if err != nil {
		return nil, err
	}

	return &ni, nil
}

func formatInfoKey(k string) string {
	return fmt.Sprintf("ssv/info/%s", k)
}
