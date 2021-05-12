package sync

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"sync"
)

// HistorySync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type HistorySync struct {
	network        network.Network
	instanceParams *proto.InstanceParams
	validatorPK    []byte
}

// NewHistorySync returns a new instance of HistorySync
func NewHistorySync(validatorPK []byte, network network.Network, instanceParams *proto.InstanceParams) *HistorySync {
	return &HistorySync{
		validatorPK:    validatorPK,
		network:        network,
		instanceParams: instanceParams,
	}
}

// Start the sync
func (s *HistorySync) Start() {
	panic("implement HistorySync")
}

// findHighestInstance returns the highest found decided signed message from peers
func (s *HistorySync) findHighestInstance() (*proto.SignedMessage, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	// TODO select peers by quality/ score?
	usedPeers, err := s.network.AllPeers(s.validatorPK)
	if err != nil {
		return nil, err
	}
	if len(usedPeers) > 4 {
		usedPeers = usedPeers[:4]
	}

	// fetch response
	wg := &sync.WaitGroup{}
	errors := make([]error, 4)
	results := make([]*network.Message, 4)
	for i, p := range usedPeers {
		go func(index int, peer peer.ID, wg *sync.WaitGroup) {
			wg.Add(1)
			res, err := s.network.GetHighestDecidedInstance(peer, &network.SyncMessage{
				Type: network.Sync_GetHighestType,
			})
			errors[index] = err
			results[index] = res
			wg.Done()
		}(i, p, wg)
	}

	wg.Wait()

	// validate response and find highest decided

	return results, errors
}

// FetchValidateAndSaveInstances fetches, validates and saves decided messages from the P2P network.
func (s *HistorySync) FetchValidateAndSaveInstances(startID []byte, endID []byte) {

}
