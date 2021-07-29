package sync

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

// HistorySync is responsible for syncing and iBFT instance when needed by
// fetching decided messages from the network
type HistorySync struct {
	logger              *zap.Logger
	publicKey           []byte
	network             network.Network
	ibftStorage         collections.Iibft
	validateDecidedMsgF func(msg *proto.SignedMessage) error
	identifier          []byte
}

// NewHistorySync returns a new instance of HistorySync
func NewHistorySync(logger *zap.Logger, publicKey []byte, identifier []byte, network network.Network, ibftStorage collections.Iibft, validateDecidedMsgF func(msg *proto.SignedMessage) error) *HistorySync {
	return &HistorySync{
		logger:              logger,
		publicKey:           publicKey,
		identifier:          identifier,
		network:             network,
		validateDecidedMsgF: validateDecidedMsgF,
		ibftStorage:         ibftStorage,
	}
}

// Start the sync
func (s *HistorySync) Start() error {
	start := time.Now()
	// fetch remote highest
	remoteHighest, fromPeer, err := s.findHighestInstance()
	if err != nil {
		return errors.Wrap(err, "could not fetch highest instance during sync")
	}
	if remoteHighest == nil { // could not find highest, there isn't one
		s.logger.Info("node is synced: could not find any peer with highest decided, assuming sequence number is 0", zap.String("duration", time.Since(start).String()))
		return nil
	}

	// fetch local highest
	localHighest, err := s.ibftStorage.GetHighestDecidedInstance(s.identifier)
	if err != nil && err.Error() != kv.EntryNotFoundError { // if not found, don't continue with sync
		return errors.Wrap(err, "could not fetch local highest instance during sync")
	}

	// special case check
	if err != nil && err.Error() == kv.EntryNotFoundError && remoteHighest.Message.SeqNumber == 0 {
		if err := s.ibftStorage.SaveDecided(remoteHighest); err != nil {
			return errors.Wrap(err, "could not save decided msg during sync")
		}
		if err := s.ibftStorage.SaveHighestDecidedInstance(remoteHighest); err != nil {
			return errors.Wrap(err, "could not save highest decided msg during sync")
		}
		s.logger.Info("finished syncing", zap.Uint64("highest seq", remoteHighest.Message.SeqNumber))
		return nil
	}

	syncStartSeqNumber := uint64(0)
	if localHighest != nil {
		syncStartSeqNumber = localHighest.Message.SeqNumber
	}

	// check we are behind and need to sync
	if syncStartSeqNumber >= remoteHighest.Message.SeqNumber {
		s.logger.Info("node is synced", zap.Uint64("highest seq", syncStartSeqNumber), zap.String("duration", time.Since(start).String()))
		return nil
	}

	// fetch, validate and save missing data
	highestSaved, err := s.fetchValidateAndSaveInstances(fromPeer, syncStartSeqNumber, remoteHighest.Message.SeqNumber)
	if err != nil {
		return errors.Wrap(err, "could not fetch decided by range during sync")
	}

	// save highest
	if highestSaved != nil {
		if err := s.ibftStorage.SaveHighestDecidedInstance(highestSaved); err != nil {
			return errors.Wrap(err, "could not save highest decided msg during sync")
		}
	}

	s.logger.Info("finished syncing", zap.Uint64("highest seq", highestSaved.Message.SeqNumber), zap.String("duration", time.Since(start).String()))
	return nil
}

// findHighestInstance returns the highest found decided signed message and the peer it was received from
func (s *HistorySync) findHighestInstance() (*proto.SignedMessage, string, error) {
	// pick up to 4 peers
	// TODO - why 4? should be set as param?
	// TODO select peers by quality/ score?
	// TODO - should be changed to support multi duty
	usedPeers, err := s.network.AllPeers(s.publicKey)
	if err != nil {
		return nil, "", err
	}
	if len(usedPeers) > 4 {
		usedPeers = usedPeers[:4]
	}

	// fetch response
	wg := &sync.WaitGroup{}
	results := make([]*network.SyncMessage, 0)
	lock := sync.Mutex{}
	for i, p := range usedPeers {
		wg.Add(1)
		go func(index int, peer string, wg *sync.WaitGroup) {
			res, err := s.network.GetHighestDecidedInstance(peer, &network.SyncMessage{
				Type:   network.Sync_GetHighestType,
				Lambda: s.identifier,
			})
			if err != nil {
				s.logger.Error("received error when fetching highest decided", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
			} else {
				lock.Lock()
				results = append(results, res)
				lock.Unlock()
			}
			wg.Done()
		}(i, p, wg)
	}

	wg.Wait()

	if len(results) == 0 {
		return nil, "", errors.New("could not fetch highest decided from peers")
	}

	// validate response and find highest decided
	var ret *proto.SignedMessage
	var fromPeer string
	foundAtLeastOne := false
	for _, res := range results {
		if res == nil {
			continue
		}

		// no highest decided
		if len(res.SignedMessages) == 0 {
			continue
		}
		foundAtLeastOne = true

		// too many responses, invalid
		if len(res.SignedMessages) > 1 {
			s.logger.Debug("received invalid highest decided", zap.Error(err),
				zap.String("identifier", hex.EncodeToString(s.identifier)))
			continue
		}

		signedMsg := res.SignedMessages[0]

		// validate
		if err := s.validateDecidedMsgF(signedMsg); err != nil {
			s.logger.Debug("received invalid highest decided", zap.Error(err),
				zap.String("identifier", hex.EncodeToString(s.identifier)))
			continue
		}

		if ret == nil {
			ret = signedMsg
			fromPeer = res.FromPeerID
		}
		if ret.Message.SeqNumber < signedMsg.Message.SeqNumber {
			ret = signedMsg
			fromPeer = res.FromPeerID
		}
	}

	// if all responses had no decided msgs and no errors than we don't have any highest decided.
	if !foundAtLeastOne {
		return nil, "", nil
	}

	// if we did find at least one response with a decided msg but all were invalid we return an error
	if ret == nil {
		s.logger.Debug("could not fetch highest decided from peers",
			zap.String("identifier", hex.EncodeToString(s.identifier)))
		return nil, "", errors.New("could not fetch highest decided from peers")
	}

	// found a valid highest decided
	return ret, fromPeer, nil
}

// FetchValidateAndSaveInstances fetches, validates and saves decided messages from the P2P network.
// Range is start to end seq including
func (s *HistorySync) fetchValidateAndSaveInstances(fromPeer string, startSeq uint64, endSeq uint64) (highestSaved *proto.SignedMessage, err error) {
	failCount := 0
	start := startSeq
	done := false
	var latestError error
	for {
		if failCount == 5 {
			return highestSaved, latestError
		}
		if done {
			return highestSaved, nil
		}

		res, err := s.network.GetDecidedByRange(fromPeer, &network.SyncMessage{
			Lambda: s.identifier,
			Params: []uint64{start, endSeq},
			Type:   network.Sync_GetInstanceRange,
		})
		if err != nil {
			failCount++
			latestError = err
			continue
		}

		// organize signed msgs into a map where the key is the sequence number
		// This is for verifying all expected sequence numbers where returned from peer
		foundSeqs := make(map[uint64]*proto.SignedMessage)
		for _, msg := range res.SignedMessages {
			foundSeqs[msg.Message.SeqNumber] = msg
		}

		// validate and save
		for i := start; i <= endSeq; i++ {
			msg, found := foundSeqs[i]
			if !found {
				failCount++
				latestError = errors.Errorf("returned decided by range messages miss sequence number %d", i)
				break
			}
			// if msg is invalid, break and try again with an updated start seq
			if s.validateDecidedMsgF(msg) != nil {
				start = msg.Message.SeqNumber
				continue
			}

			// save
			if err := s.ibftStorage.SaveDecided(msg); err != nil {
				return highestSaved, err
			}

			// set highest
			if highestSaved == nil {
				highestSaved = msg
			}
			if highestSaved.Message.SeqNumber < msg.Message.SeqNumber {
				highestSaved = msg
			}

			start = msg.Message.SeqNumber + 1

			if msg.Message.SeqNumber == endSeq {
				done = true
			}
		}
		s.logger.Info(fmt.Sprintf("fetched and saved instances up to sequence number %d", endSeq))
	}
}
