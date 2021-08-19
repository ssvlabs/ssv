package sync

import "github.com/bloxapp/ssv/network"

// GetPeers returns an array of peers selected
func GetPeers(net network.Network, pk []byte, maxPeerCount int) ([]string, error) {
	// TODO select peers by quality/ score?
	// TODO - should be changed to support multi duty
	usedPeers, err := net.AllPeers(pk)
	if err != nil {
		return nil, err
	}
	if len(usedPeers) > maxPeerCount {
		usedPeers = usedPeers[:maxPeerCount]
	}
	return usedPeers, nil
}
