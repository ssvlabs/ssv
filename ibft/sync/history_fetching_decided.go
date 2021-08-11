package sync

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
)

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
