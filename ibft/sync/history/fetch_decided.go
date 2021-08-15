package history

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
)

// getHighestDecidedFromPeers receives highest decided messages from peers
func (s *Sync) getHighestDecidedFromPeers(peers []string) []*network.SyncMessage {
	var results []*network.SyncMessage
	var wg sync.WaitGroup
	var lock sync.Mutex

	// peer's highest decided message will be added to results if:
	//  1. not-found-error (i.e. no history)
	//  3. message is valid
	for i, p := range peers {
		wg.Add(1)
		go func(index int, peer string, wg *sync.WaitGroup) {
			defer wg.Done()
			res, err := s.network.GetHighestDecidedInstance(peer, &network.SyncMessage{
				Type:   network.Sync_GetHighestType,
				Lambda: s.identifier,
			})
			if err != nil {
				s.logger.Error("received error when fetching highest decided", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
				return
			}
			// backwards compatibility for version < v0.0.12, where EntryNotFoundError didn't exist
			// TODO: can be removed once the release is deprecated
			if len(res.SignedMessages) == 0 {
				res.Error = kv.EntryNotFoundError
			}

			if len(res.Error) > 0 {
				// assuming not found is a valid scenario (e.g. new validator)
				// therefore we count the result now, and it will be identified afterwards in findHighestInstance()
				if res.Error == kv.EntryNotFoundError {
					lock.Lock()
					results = append(results, res)
					lock.Unlock()
				} else {
					s.logger.Error("received error when fetching highest decided", zap.Error(err),
						zap.String("identifier", hex.EncodeToString(s.identifier)))
				}
				return
			}

			if len(res.SignedMessages) != 1 {
				s.logger.Debug("received multiple signed messages", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
				return
			}
			if err := s.validateDecidedMsgF(res.SignedMessages[0]); err != nil {
				s.logger.Debug("received invalid highest decided", zap.Error(err),
					zap.String("identifier", hex.EncodeToString(s.identifier)))
				return
			}

			lock.Lock()
			results = append(results, res)
			lock.Unlock()
		}(i, p, &wg)
	}
	wg.Wait()

	return results
}

// FetchValidateAndSaveInstances fetches, validates and saves decided messages from the P2P network.
// Range is start to end seq including
func (s *Sync) fetchValidateAndSaveInstances(fromPeer string, startSeq uint64, endSeq uint64) (highestSaved *proto.SignedMessage, err error) {
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

		msgCount := len(res.SignedMessages)
		// validate and save
		for i := start; i <= endSeq; i++ {
			msg, found := foundSeqs[i]
			if !found {
				failCount++
				latestError = errors.Errorf("returned decided by range messages miss sequence number %d", i)
				s.logger.Debug("decided by range messages miss sequence number",
					zap.Uint64("seq", i), zap.Int("msgCount", msgCount))
				break
			}
			// counting all the messages that were visited
			msgCount--
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
			if highestSaved == nil || highestSaved.Message.SeqNumber < msg.Message.SeqNumber {
				highestSaved = msg
			}

			start = msg.Message.SeqNumber + 1

			if msg.Message.SeqNumber == endSeq {
				done = true
			}
			// if the current messages batch was processed -> break loop and start the next batch
			if msgCount == 0 {
				break
			}
		}
		s.logger.Info(fmt.Sprintf("fetched and saved instances up to sequence number %d", start-1))
	}
}
