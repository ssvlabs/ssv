package history

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// FetchValidateAndSaveInstances fetches, validates and saves decided messages from the P2P network.
// Range is start to end seq including
func (s *Sync) fetchValidateAndSaveInstances(fromPeer string, startSeq uint64, endSeq uint64) (highestSaved *proto.SignedMessage, n int, err error) {
	failCount := 0
	start := startSeq
	done := false
	var latestError error
	for {
		if failCount == 5 {
			return highestSaved, n, latestError
		}
		if done {
			return highestSaved, n, nil
		}

		// conform to max batch
		batchMaxSeq := start + s.paginationMaxSize
		if batchMaxSeq > endSeq {
			batchMaxSeq = endSeq
		}

		res, err := s.network.GetDecidedByRange(fromPeer, &network.SyncMessage{
			Lambda: s.identifier,
			Params: []uint64{start, batchMaxSeq},
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

		s.logger.Info(fmt.Sprintf("fetching sequences %d - %d from peer", start, batchMaxSeq), zap.String("peer", fromPeer))

		msgCount := len(res.SignedMessages)
		// validate and save
		for i := start; i <= batchMaxSeq; i++ {
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
				return highestSaved, n, err
			}
			n++

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
	}
}
