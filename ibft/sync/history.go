package sync

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
)

// HistorySync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type HistorySync struct {
	logger         *zap.Logger
	network        network.Network
	instanceParams *proto.InstanceParams
	validatorPK    []byte
}

// NewHistorySync returns a new instance of HistorySync
func NewHistorySync(validatorPK []byte, network network.Network, instanceParams *proto.InstanceParams, logger *zap.Logger) *HistorySync {
	return &HistorySync{
		logger:         logger,
		validatorPK:    validatorPK,
		network:        network,
		instanceParams: instanceParams,
	}
}

// Start the sync
func (s *HistorySync) Start() {
	_, err := s.findHighestInstance()
	if err != nil {
		panic("implement")
	}
	panic("implement HistorySync")
}

// findHighestInstance returns the highest found decided signed message from peers
func (s *HistorySync) findHighestInstance() (*proto.SignedMessage, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	// TODO select peers by quality/ score?
	// TODO - should be changed to support multi duty
	usedPeers, err := s.network.AllPeers(s.validatorPK)
	if err != nil {
		return nil, err
	}
	if len(usedPeers) > 4 {
		usedPeers = usedPeers[:4]
	}

	// fetch response
	wg := &sync.WaitGroup{}
	results := make([]*network.Message, 4)
	for i, p := range usedPeers {
		wg.Add(1)
		go func(index int, peer peer.ID, wg *sync.WaitGroup) {
			res, err := s.network.GetHighestDecidedInstance(peer, &network.SyncMessage{
				Type:        network.Sync_GetHighestType,
				ValidatorPk: s.validatorPK,
			})
			if err != nil {
				s.logger.Error("received error when fetching highest decided", zap.Error(err))
			} else {
				results[index] = res
			}
			wg.Done()
		}(i, p, wg)
	}

	wg.Wait()

	// validate response and find highest decided
	var ret *proto.SignedMessage
	for _, res := range results {
		if res == nil {
			continue
		}

		// validate
		if err := s.isValidHighestMsg(res.SignedMessage); err != nil {
			s.logger.Debug("received invalid highest decided", zap.Error(err))
			continue
		}

		if ret == nil {
			ret = res.SignedMessage
		}
		if ret.Message.SeqNumber < res.SignedMessage.Message.SeqNumber {
			ret = res.SignedMessage
		}
	}

	if ret == nil {
		return nil, errors.New("could not fetch highest decided from peers")
	}

	return ret, nil
}

func (s *HistorySync) isValidHighestMsg(msg *proto.SignedMessage) error {
	// signature
	if err := s.instanceParams.VerifySignedMessage(msg); err != nil {
		return err
	}

	// threshold
	if len(msg.SignerIds) < s.instanceParams.ThresholdSize() {
		return errors.New("highest msg has no quorum")
	}

	// validator pk
	if !bytes.Equal(s.validatorPK, msg.Message.ValidatorPk) {
		return errors.New("invalid validator PK")
	}
	return nil
}

// FetchValidateAndSaveInstances fetches, validates and saves decided messages from the P2P network.
func (s *HistorySync) FetchValidateAndSaveInstances(startID []byte, endID []byte) {

}
