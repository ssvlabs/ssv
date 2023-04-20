package validation

import (
	"fmt"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"oya.to/namedlocker"
)

type messageID string

const (
	// minDecidedInterval is the minimum interval between two decided messages.
	minDecidedInterval = time.Second * 2

	// roundSlack is the number of acceptable past rounds.
	roundSlack = 0

	// maxNetworkLatency is the maximum network latency we tolerate.
	maxNetworkLatency = time.Millisecond * 1750
)

// messageSchedule keeps track of consensus msg schedules to determine timely receiving msgs
type messageSchedule struct {
	//map id -> map signer -> mark
	marks *marksCollection
	sto   *namedlocker.Store
}

func newMessageSchedule() *messageSchedule {
	return &messageSchedule{
		marks: newMarksCollection(),
		sto:   &namedlocker.Store{},
	}
}

// MarkConsensusMessage marks a msg
func (schedule *messageSchedule) MarkConsensusMessage(logger *zap.Logger, sm *signerMark, identifier []byte, signer types.OperatorID, round qbft.Round, msgType qbft.MessageType) {
	logger.Info("marking consensus message", zap.Int("msgType", int(msgType)), zap.Any("signer", signer))

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.HighestRound < round {
		sm.ResetForNewRound(round, msgType)
	} else {
		occurrencesByType, exists := sm.MsgTypesInRound.Get(round)
		if !exists {
			// this wasn't called in staging
			logger.Warn("round missing from signer msgType Map")
			occurrencesByType = hashmap.New[qbft.MessageType, int]()
			occurrencesByType.Set(msgType, 1)
			sm.MsgTypesInRound.Set(round, occurrencesByType)
		} else {
			occurrences, exists := occurrencesByType.Get(msgType)
			logger.Debug("occurrences", zap.Int("occurrences", occurrences), zap.Bool("occured", exists))
			if !exists {
				occurrencesByType.Set(msgType, 1)
			} else {
				occurrencesByType.Set(msgType, occurrences+1)
			}
		}
	}
}

func (signerMark *mark) isConsensusMsgTimely(msg *qbft.SignedMessage, plog *zap.Logger, minRoundTime time.Time) (bool, pubsub.ValidationResult) {
	return signerMark.isConsensusMessageTimely(msg.Message.Height, msg.Message.Round, msg.Message.MsgType, msg.GetSigners(), minRoundTime, plog)
}

func (signerMark *mark) isConsensusMessageTimely(height qbft.Height, round qbft.Round, msgType qbft.MessageType, signers []types.OperatorID, minRoundTime time.Time, logger *zap.Logger) (bool, pubsub.ValidationResult) {
	signerMark.mu.RLock()
	defer signerMark.mu.RUnlock()
	logger = logger.With(zap.Any("markedRound", signerMark.HighestRound), zap.Any("markedHeight", signerMark.HighestDecided)).
		With(zap.Any("round", round))

	if signerMark.HighestDecided > height {
		logger.Warn("past height", zap.Any("height", height))
		return false, pubsub.ValidationReject
	} else if signerMark.HighestDecided == height {
		// if a commit message was received for a decided round, but we do not know about it, then we should process it
		// TODO there's an attack vector, someone can try to create commits for a decided round and we will process them
		// TODO check height == 0 case
		if msgType == qbft.CommitMsgType && signerMark.DecidedRound == round && signerMark.signers[signers[0]] != struct{}{} {
			logger.Warn("a commit message that should help create a better decided", zap.Any("height", height))
			return true, pubsub.ValidationAccept
		}
		// TODO: ugly hack for height == 0 case
		// Problematic if another commit comes to make a better decided
		if height != 0 || signerMark.MarkedDecided > 0 {
			logger.Warn("a qbft message arrived for the same height was already decided", zap.Any("height", height))
			// assume a late message and don't penalize
			// TODO maybe penalize every 2-3 messages?
			return false, pubsub.ValidationIgnore
		}
		// TODO: Buggy Hack! Perhaps we can fork the network so height 0 is Bootstrap_Height or no Height!!
	} else if signerMark.HighestDecided != height-1 && signerMark.HighestDecided != 0 {
		logger.Warn("future height")
		// TODO assume that theres should be enough time between duties so that we don't see out of order messages
		// This is problematic in case we miss a decided message.
		// Maybe on the next decided message this will fix itself?
		return false, pubsub.ValidationReject
	}

	if signerMark.HighestRound < round {
		// if new round msg, check at least round timeout has passed
		//TODO research this time better - what happens with proposals timeout?
		// TODO: maybe inject function in a better way
		if time.Now().After(minRoundTime.Add(roundtimer.RoundTimeout(signerMark.HighestRound) - maxNetworkLatency)) {
			logger.Debug("timely round expiration", zap.Time("firstMsgInRound", minRoundTime))
			return true, pubsub.ValidationAccept
		}
		// TODO occurs in staging, why?
		logger.Warn("not timely round expiration", zap.Time("firstMsgInRound", minRoundTime))
		return false, pubsub.ValidationReject
	} else if signerMark.HighestRound == round {
		// tooManyMessages check later on may change the result
		return true, pubsub.ValidationAccept
	} else {
		// past rounds are not timely
		// TODO: Important!!! Note: investigate what happens when a malicious signer signs a future round.
		// it may be fine since markers are per signer
		// one solution is to have threshold of signers update the marker's round. But are we guaranteed to see all the messages?
		logger.Warn("past round")
		// TODO research this slack. Consider using ValidationIgnore
		if round+roundSlack >= signerMark.HighestRound {
			logger.Warn("past round but within slack", zap.Any("slack", roundSlack))
			// TODO change to ValidationIgnore?
			return true, pubsub.ValidationAccept
		}
		return false, pubsub.ValidationReject
	}
}

func (schedule *messageSchedule) markDecidedMsg(signedMsg *qbft.SignedMessage, share *ssvtypes.SSVShare, plog *zap.Logger) {
	id := signedMsg.Message.Identifier
	var signers []types.OperatorID
	// All members of the committee should be marked. Even ones that didn't sign.
	// This is because non-signers should get marked so they move to the next height.
	for _, operator := range share.Committee {
		signer := operator.OperatorID
		signers = append(signers, signer)
	}
	height := signedMsg.Message.Height
	round := signedMsg.Message.Round

	schedule.markDecidedMessage(id, signers, height, round, signedMsg.Signers, plog)
}

func (schedule *messageSchedule) markDecidedMessage(id []byte, committeeMembers []types.OperatorID, height qbft.Height, round qbft.Round, signatures []types.OperatorID, plog *zap.Logger) {
	for _, signer := range committeeMembers {
		lockID := fmt.Sprintf("%#x-%d", id, signer)
		schedule.sto.Lock(lockID)
		defer schedule.sto.Unlock(lockID)
		sm, _ := schedule.marks.GetOrCreateSignerMark(messageID(id), signer)
		sm.updateDecidedMark(height, round, signatures, plog)
		plog.Info("decided mark updated", zap.Any("mark signer", signer), zap.Any("decided mark", sm))
	}
}

// isTimelyDecidedMsg returns true if decided message is timely (both for future and past decided messages)
// FUTURE: when a valid decided msg is received, the next duty for the runner is marked. The next decided message will not be validated before that time.
// Aims to prevent a byzantine committee rapidly broadcasting decided messages
// PAST: a decided message which is "too" old will be rejected as well
func (schedule *messageSchedule) isTimelyDecidedMsg(msg *qbft.SignedMessage, plog *zap.Logger) bool {
	return schedule.isTimelyDecidedMessage(msg.Message.Identifier, msg.Signers, msg.Message.Height, plog)
}

func (schedule *messageSchedule) isTimelyDecidedMessage(id []byte, signers []types.OperatorID, height qbft.Height, logger *zap.Logger) bool {
	logger.Info("checking if decided message is timely")
	// TODO maybe we should check if all signers are in the committee (should be done in semantic checks)? - low priority
	//
	for _, signer := range signers {
		sm, found := schedule.marks.GetSignerMark(messageID(id), signer)
		if !found {
			// if one mark is missing it is enough to declare message as timely
			return true
		}

		sm.mu.RLock()
		defer sm.mu.RUnlock()

		logger.With(zap.Any("markedHeight", sm.HighestDecided))
		// TODO is this a problem when we sync?
		if sm.HighestDecided > height {
			logger.Warn("decided message is too old", zap.Int("markedHeight", int(sm.HighestDecided)))
			// maybe for other signers it is timely
			continue
		}
		if sm.HighestDecided == height {
			// This passed the hasBetterOrSimilarCheck so it is always timely
			//TODO maybe move betterOrSimilarMsgCount logic here?
			return true
		}
		fromLast2Decided := sm.DurationFromLast2Decided()
		logger.Warn("duration from last 2 decided",
			zap.Duration("duration", fromLast2Decided), zap.Any("markedHeight", sm.HighestDecided), zap.Any("signer", signer))
		// TODO research this time better
		if fromLast2Decided >= minDecidedInterval {
			return true
		}
	}
	return false
}

func (schedule *messageSchedule) minFirstMsgTimeForRound(signedMsg *qbft.SignedMessage, round qbft.Round, plogger *zap.Logger) time.Time {
	marks, _ := schedule.marks.Get(messageID(signedMsg.Message.Identifier))
	minRoundTime := time.Time{}
	marks.signerMarks.Range(func(signer types.OperatorID, mark *signerMark) bool {
		mark.mu.RLock()
		defer mark.mu.RUnlock()
		if mark.FirstMsgInRound == nil {
			plogger.Debug("first msg in round is nil", zap.Any("other signer", signer))
			return true
		}

		firstMsgInRoundTime, exists := mark.FirstMsgInRound.Get(round)
		if !exists {
			plogger.Debug("minFirstMsgTimeForRound - first msg in round is not found", zap.Any("other signer", signer))
			return true
		}

		if minRoundTime.IsZero() || firstMsgInRoundTime.Before(minRoundTime) {
			minRoundTime = firstMsgInRoundTime
		}
		return true
	})
	return minRoundTime
}

// TODO maybe similar messages should pass
// hasBetterOrSimilarMsg returns true if there is a decided message with the same height and the same or more signatures
func (schedule *messageSchedule) hasBetterOrSimilarMsg(msg *qbft.SignedMessage, quorum, committeeSize uint64, peerID peer.ID) (bool, pubsub.ValidationResult) {
	messageMark, found := schedule.marks.Get(messageID(msg.Message.Identifier))
	if !found {
		return false, pubsub.ValidationAccept
	}

	validation := pubsub.ValidationAccept
	betterOrSimilarMsg := false
	messageMark.signerMarks.Range(func(key types.OperatorID, sm *signerMark) bool {
		sm.mu.RLock()
		defer sm.mu.RUnlock()

		if sm.HighestDecided != msg.Message.Height || len(msg.Signers) > len(maps.Keys(sm.signers)) {
			return true
		}

		pm, _ := sm.peerMarks.GetOrInsert(peerID, &mark{HighestDecided: msg.Message.Height})

		if pm.HighestDecided == msg.Message.Height {
			if pm.MarkedDecided >= getBetterOrSimilarThreshold(quorum, committeeSize) {
				validation = pubsub.ValidationReject
				return false
			}
			// in else clause for overflow protection (even though not sure this is possible)
			pm.MarkedDecided++
		}
		// continue to check other signers, maybe they will be rejected
		validation = pubsub.ValidationIgnore
		return true
	})

	return betterOrSimilarMsg, validation
}

func (signerMark *mark) tooManyMessagesPerRound(msg *qbft.SignedMessage, share *ssvtypes.SSVShare, logger *zap.Logger) bool {
	signerMark.mu.RLock()
	defer signerMark.mu.RUnlock()

	logger.Info("checking if too many messages per round", zap.Any("round", msg.Message.Round), zap.Any("msgType", msg.Message.MsgType))
	if signerMark.MsgTypesInRound == nil {
		signerMark.MsgTypesInRound = hashmap.New[qbft.Round, *hashmap.Map[qbft.MessageType, int]]()
	}
	msgTypeToOccurrences, found := signerMark.MsgTypesInRound.Get(msg.Message.Round)
	if !found {
		logger.Info("mark was not initialized for round")
		// must not initialize inner map here because memory allocation should happen only after signature validation
		return false
	}
	_, exists := msgTypeToOccurrences.Get(msg.Message.MsgType)

	return exists
}

// in actual implementation, instead of calculating we can have a lookup table for supported committee sizes
func getBetterOrSimilarThreshold(quorum, committeeSize uint64) int {
	threshold := 0
	for k := quorum; k <= committeeSize; k++ {
		//TODO: we sure order doesn't matter?
		// see https://en.wikipedia.org/wiki/Combination
		threshold += choose(int(committeeSize), int(k))
	}
	return threshold
}

// choose calculates the binomial coefficient C(n, k) using int.
func choose(n, k int) int {
	if k > n {
		return 0
	}

	k = min(k, n-k) // C(n, k) = C(n, n-k)
	if k == 0 {
		return 1
	}

	result := 1
	for i := 1; i <= k; i++ {
		result *= n - i + 1
		result /= i
	}

	return result
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
