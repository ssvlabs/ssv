package sync

import (
	"bytes"
	"errors"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
	"sync"
)

// HistorySync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type HistorySync struct {
	logger         *zap.Logger
	network        network.Network
	ibftStorage    collections.Iibft
	instanceParams *proto.InstanceParams
	validatorPK    []byte
}

// NewHistorySync returns a new instance of HistorySync
func NewHistorySync(validatorPK []byte, network network.Network, ibftStorage collections.Iibft, instanceParams *proto.InstanceParams, logger *zap.Logger) *HistorySync {
	return &HistorySync{
		logger:         logger,
		validatorPK:    validatorPK,
		network:        network,
		ibftStorage:    ibftStorage,
		instanceParams: instanceParams,
	}
}

// Start the sync
func (s *HistorySync) Start() {
	// fetch remote highest
	remoteHighest, fromPeer, err := s.findHighestInstance()
	if err != nil {
		s.logger.Error("could not fetch highest instance during sync", zap.Error(err))
		return
	}

	// fetch local highest
	localHighest, err := s.ibftStorage.GetHighestDecidedInstance(s.validatorPK)
	if err != nil && err.Error() != collections.EntryNotFoundError { // if not found continue with sync
		s.logger.Error("could not fetch local highest instance during sync", zap.Error(err))
		return
	}

	syncStartSeqNumber := uint64(0)
	if localHighest != nil {
		syncStartSeqNumber = localHighest.Message.SeqNumber + 1
	}

	// check we are behind and need to sync
	if syncStartSeqNumber >= remoteHighest.Message.SeqNumber {
		s.logger.Info("node is synced", zap.Uint64("highest seq", syncStartSeqNumber))
		return
	}

	// fetch missing data
	decidedMsgs, err := s.fetchValidateAndSaveInstances(fromPeer, syncStartSeqNumber, remoteHighest.Message.SeqNumber)
	if err != nil {
		s.logger.Error("could not fetch decided by range during sync", zap.Error(err))
		return
	}

	// save to storage
	var fetchedHighest *proto.SignedMessage
	for _, msg := range decidedMsgs {
		if err := s.ibftStorage.SaveDecided(msg); err != nil {
			s.logger.Error("could not save decided msg during sync", zap.Error(err))
			break
		}

		// set highest
		if fetchedHighest == nil {
			fetchedHighest = msg
		}
		if msg.Message.SeqNumber > fetchedHighest.Message.SeqNumber {
			fetchedHighest = msg
		}
	}

	// save highest
	if err := s.ibftStorage.SaveHighestDecidedInstance(fetchedHighest); err != nil {
		s.logger.Error("could not save highest decided msg during sync", zap.Error(err))
	}
}

// findHighestInstance returns the highest found decided signed message and the peer it was received from
func (s *HistorySync) findHighestInstance() (*proto.SignedMessage, peer.ID, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	// TODO select peers by quality/ score?
	// TODO - should be changed to support multi duty
	usedPeers, err := s.network.AllPeers(s.validatorPK)
	if err != nil {
		return nil, "", err
	}
	if len(usedPeers) > 4 {
		usedPeers = usedPeers[:4]
	}

	// fetch response
	wg := &sync.WaitGroup{}
	results := make([]*network.SyncMessage, 4)
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
	var fromPeer peer.ID
	for _, res := range results {
		if res == nil {
			continue
		}

		if len(res.SignedMessages) != 1 || res.SignedMessages[0] == nil {
			s.logger.Debug("received invalid highest decided", zap.Error(err))
			continue
		}

		signedMsg := res.SignedMessages[0]

		// validate
		if err := s.isValidDecidedMsg(signedMsg); err != nil {
			s.logger.Debug("received invalid highest decided", zap.Error(err))
			continue
		}

		if ret == nil {
			ret = signedMsg
			fromPeer = peer.ID(res.FromPeerID)
		}
		if ret.Message.SeqNumber < signedMsg.Message.SeqNumber {
			ret = signedMsg
			fromPeer = peer.ID(res.FromPeerID)
		}
	}

	if ret == nil {
		return nil, "", errors.New("could not fetch highest decided from peers")
	}

	return ret, fromPeer, nil
}

func (s *HistorySync) isValidDecidedMsg(msg *proto.SignedMessage) error {
	// TODO - test lambda? prev lambda?

	if msg.Message.Type != proto.RoundState_Decided {
		return errors.New("decided msg with wrong type")
	}

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
// Range is start to end seq including
func (s *HistorySync) fetchValidateAndSaveInstances(fromPeer peer.ID, startSeq uint64, endSeq uint64) ([]*proto.SignedMessage, error) {
	ret := make([]*proto.SignedMessage, endSeq-startSeq+1)
	failCount := 0
	start := startSeq
	done := false
	for {
		if failCount == 5 {
			return nil, errors.New("could not fetch ranged decided instances")
		}
		if done {
			return ret, nil
		}

		res, err := s.network.GetDecidedByRange(fromPeer, &network.SyncMessage{
			ValidatorPk: s.validatorPK,
			Params:      []uint64{start, endSeq},
			Type:        network.Sync_GetInstanceRange,
		})
		if err != nil {
			failCount++
			continue
		}

		// set in return slice
		for _, msg := range res.SignedMessages {
			// if msg is invalid, break and try again with an updated start seq
			if s.isValidDecidedMsg(msg) != nil {
				start = msg.Message.SeqNumber
				continue
			}
			saveIndex := msg.Message.SeqNumber - startSeq
			ret[saveIndex] = msg
			start = msg.Message.SeqNumber + 1

			if msg.Message.SeqNumber == endSeq {
				done = true
			}
		}
	}
}
